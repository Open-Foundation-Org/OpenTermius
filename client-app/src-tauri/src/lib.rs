use std::collections::HashMap;
use std::io::{Read, Write};
use std::path::PathBuf;
use std::sync::{Arc, Mutex, OnceLock};
use std::time::{Duration, UNIX_EPOCH};

use portable_pty::{native_pty_system, CommandBuilder, MasterPty, PtySize};
use russh::keys::{decode_secret_key, ssh_key, PrivateKey, PrivateKeyWithHashAlg, PublicKeyOrCertificate};
use russh::{client, ChannelMsg, ChannelWriteHalf, Disconnect};
use russh_sftp::client::SftpSession;
use serde::{Deserialize, Serialize};
use tauri::{AppHandle, Emitter, State};
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpListener;

static KNOWN_HOSTS: OnceLock<Mutex<HashMap<String, String>>> = OnceLock::new();
static PENDING_HOST_KEYS: OnceLock<Mutex<HashMap<String, String>>> = OnceLock::new();

fn pending_host_keys_map() -> &'static Mutex<HashMap<String, String>> {
    PENDING_HOST_KEYS.get_or_init(|| Mutex::new(HashMap::new()))
}

fn extract_host_key_info(pk: &PublicKeyOrCertificate) -> (String, String) {
    match pk {
        PublicKeyOrCertificate::PublicKey { key, .. } => {
            let algo = match key.algorithm() {
                ssh_key::Algorithm::Ed25519 => "ED25519".to_string(),
                ssh_key::Algorithm::Rsa { .. } => "RSA".to_string(),
                ssh_key::Algorithm::Ecdsa { .. } => "ECDSA".to_string(),
                other => other.as_str().to_uppercase(),
            };
            let fp = key.fingerprint(ssh_key::HashAlg::Sha256).to_string();
            (algo, fp)
        }
        PublicKeyOrCertificate::Certificate(cert) => {
            let pub_key = ssh_key::PublicKey::from(cert.public_key().clone());
            let fp = pub_key.fingerprint(ssh_key::HashAlg::Sha256).to_string();
            ("CERT".to_string(), fp)
        }
    }
}

fn user_home_dir() -> Option<PathBuf> {
    std::env::var("HOME")
        .or_else(|_| std::env::var("USERPROFILE"))
        .ok()
        .map(PathBuf::from)
        .filter(|p| p.exists())
}

fn known_hosts_path() -> Option<PathBuf> {
    user_home_dir()
        .or_else(|| Some(std::env::temp_dir()))
        .map(|p| p.join(".opentermius_known_hosts.json"))
}

fn known_hosts_map() -> &'static Mutex<HashMap<String, String>> {
    KNOWN_HOSTS.get_or_init(|| {
        let map = known_hosts_path()
            .and_then(|p| std::fs::read_to_string(p).ok())
            .and_then(|s| serde_json::from_str::<HashMap<String, String>>(&s).ok())
            .unwrap_or_default();
        Mutex::new(map)
    })
}

fn persist_known_hosts(map: &HashMap<String, String>) {
    if let Some(path) = known_hosts_path() {
        if let Ok(json) = serde_json::to_string_pretty(map) {
            let _ = std::fs::write(path, json);
        }
    }
}

enum HostKeyCheck {
    Trusted,
    Unknown,
    Mismatch,
}

fn check_known_host(host_port: &str, key_repr: &str) -> HostKeyCheck {
    let Ok(map) = known_hosts_map().lock() else {
        return HostKeyCheck::Trusted;
    };

    match map.get(host_port) {
        Some(existing) if existing == key_repr => HostKeyCheck::Trusted,
        Some(_) => HostKeyCheck::Mismatch,
        None => HostKeyCheck::Unknown,
    }
}

fn normalize_host_port(raw_host: &str, default_port: u16) -> (String, u16) {
    let trimmed = raw_host.trim();
    if let Some(rest) = trimmed.strip_prefix('[') {
        if let Some((ipv6, port_part)) = rest.split_once(']') {
            if let Some(p_str) = port_part.strip_prefix(':') {
                if let Ok(p) = p_str.trim().parse::<u16>() {
                    return (ipv6.to_string(), p);
                }
            }
            return (ipv6.to_string(), default_port);
        }
    }
    if trimmed.matches(':').count() == 1 {
        if let Some((h, p_str)) = trimmed.rsplit_once(':') {
            if let Ok(p) = p_str.trim().parse::<u16>() {
                return (h.trim().to_string(), p);
            }
        }
    }
    (trimmed.to_string(), default_port)
}

fn drain_utf8_chunk(pending: &mut Vec<u8>, incoming: &[u8]) -> String {
    pending.extend_from_slice(incoming);
    let mut out = String::new();
    loop {
        match std::str::from_utf8(pending) {
            Ok(valid) => {
                out.push_str(valid);
                pending.clear();
                break;
            }
            Err(e) => {
                let valid_len = e.valid_up_to();
                if valid_len > 0 {
                    if let Ok(valid) = std::str::from_utf8(&pending[..valid_len]) {
                        out.push_str(valid);
                    }
                }
                match e.error_len() {
                    Some(invalid_len) => {
                        out.push('\u{FFFD}');
                        pending.drain(..valid_len + invalid_len);
                    }
                    None => {
                        pending.drain(..valid_len);
                        break;
                    }
                }
            }
        }
    }
    out
}

struct SshClientHandler {
    host_port: String,
    key_status: Arc<Mutex<Option<(String, String, String)>>>,
}

impl client::Handler for SshClientHandler {
    type Error = russh::Error;

    async fn check_server_key(
        &mut self,
        server_public_key: &PublicKeyOrCertificate,
    ) -> Result<bool, Self::Error> {
        let key_repr = format!("{:?}", server_public_key);
        match check_known_host(&self.host_port, &key_repr) {
            HostKeyCheck::Trusted => Ok(true),
            HostKeyCheck::Unknown => {
                let (algo, fp) = extract_host_key_info(server_public_key);
                if let Ok(mut pending) = pending_host_keys_map().lock() {
                    pending.insert(self.host_port.clone(), key_repr);
                }
                if let Ok(mut st) = self.key_status.lock() {
                    *st = Some(("HOST_KEY_UNKNOWN".to_string(), algo, fp));
                }
                Ok(false)
            }
            HostKeyCheck::Mismatch => {
                let (algo, fp) = extract_host_key_info(server_public_key);
                if let Ok(mut pending) = pending_host_keys_map().lock() {
                    pending.insert(self.host_port.clone(), key_repr);
                }
                if let Ok(mut st) = self.key_status.lock() {
                    *st = Some(("HOST_KEY_MISMATCH".to_string(), algo, fp));
                }
                Ok(false)
            }
        }
    }
}

#[tauri::command]
async fn ssh_trust_host_key(
    sftp_pool: State<'_, SftpPoolState>,
    host: String,
    port: Option<u16>,
) -> Result<(), String> {
    let (clean_host, actual_port) = normalize_host_port(&host, port.unwrap_or(22));
    let key = format!("{}:{}", clean_host, actual_port);
    let pending_repr = pending_host_keys_map()
        .lock()
        .ok()
        .and_then(|mut p| p.remove(&key));

    if let Ok(mut map) = known_hosts_map().lock() {
        if let Some(repr) = pending_repr {
            map.insert(key.clone(), repr);
        } else {
            map.remove(&key);
        }
        persist_known_hosts(&map);
    }
    let prefix = format!("{}:", key);
    let mut sessions = sftp_pool.sessions.lock().await;
    sessions.retain(|k, _| !k.starts_with(&prefix));
    Ok(())
}

enum SessionHandle {
    LocalPty {
        writer: Arc<Mutex<Box<dyn Write + Send>>>,
        master: Arc<Mutex<Box<dyn MasterPty + Send>>>,
        child: Arc<Mutex<Box<dyn portable_pty::Child + Send + Sync>>>,
    },
    RemoteSsh {
        write_half: Arc<tokio::sync::Mutex<ChannelWriteHalf<client::Msg>>>,
        session: Arc<tokio::sync::Mutex<client::Handle<SshClientHandler>>>,
    },
}

#[derive(Default)]
pub struct TerminalState {
    sessions: Arc<tokio::sync::Mutex<HashMap<String, SessionHandle>>>,
}

#[derive(Deserialize)]
pub struct TerminalOpenConfig {
    pub session_id: String,
    pub host: Option<String>,
    pub port: Option<u16>,
    pub username: Option<String>,
    pub password: Option<String>,
    pub private_key: Option<String>,
    pub passphrase: Option<String>,
    pub cols: Option<u16>,
    pub rows: Option<u16>,
}

async fn establish_ssh_session(
    host: &str,
    port: u16,
    username: &str,
    password: Option<&str>,
    private_key: Option<&str>,
    passphrase: Option<&str>,
) -> Result<client::Handle<SshClientHandler>, String> {
    let (clean_host, actual_port) = normalize_host_port(host, port);
    let config = Arc::new(client::Config {
        inactivity_timeout: None,
        keepalive_interval: Some(Duration::from_secs(15)),
        keepalive_max: 8,
        ..Default::default()
    });

    let addr = (clean_host.as_str(), actual_port);
    let mut last_err = String::new();
    let mut connected_session: Option<client::Handle<SshClientHandler>> = None;

    for attempt in 0..3 {
        let key_status = Arc::new(Mutex::new(None));
        let handler = SshClientHandler {
            host_port: format!("{}:{}", clean_host, actual_port),
            key_status: key_status.clone(),
        };
        let res = tokio::time::timeout(
            Duration::from_secs(15),
            client::connect(config.clone(), addr, handler),
        )
        .await;

        match res {
            Ok(Ok(s)) => {
                connected_session = Some(s);
                break;
            }
            Ok(Err(e)) => {
                if let Ok(guard) = key_status.lock() {
                    if let Some((kind, algo, fp)) = guard.as_ref() {
                        return Err(format!("{}|{}|{}|{}|{}", kind, clean_host, actual_port, algo, fp));
                    }
                }
                let err_str = e.to_string();
                let is_route_or_arp = err_str.contains("os error 65")
                    || err_str.to_lowercase().contains("no route to host")
                    || err_str.to_lowercase().contains("hostunreachable");
                if is_route_or_arp && attempt < 2 {
                    tokio::time::sleep(Duration::from_millis(450)).await;
                    continue;
                }
                #[cfg(target_os = "macos")]
                if is_route_or_arp {
                    last_err = format!(
                        "SSH connection failed to {}:{}: {} (On macOS: check System Settings → Privacy & Security → Local Network → toggle OpenTermius OFF then ON)",
                        clean_host, actual_port, err_str
                    );
                    break;
                }
                last_err = format!("SSH connection failed to {}:{}: {}", clean_host, actual_port, err_str);
                break;
            }
            Err(_) => {
                last_err = format!("Connection timed out to {}:{}", clean_host, actual_port);
                break;
            }
        }
    }

    let mut session = connected_session.ok_or(last_err)?;

    let user = if username.trim().is_empty() {
        "root"
    } else {
        username.trim()
    };

    if let Some(key_pem) = private_key.filter(|k| !k.trim().is_empty()) {
        let pass = passphrase.filter(|p| !p.is_empty());
        let key_pair = decode_secret_key(key_pem.trim(), pass)
            .map_err(|e| format!("Invalid SSH private key: {}", e))?;
        let best_hash = session
            .best_supported_rsa_hash()
            .await
            .map_err(|e| format!("Failed to negotiate RSA hash: {}", e))?
            .flatten();
        let auth_res = session
            .authenticate_publickey(
                user,
                PrivateKeyWithHashAlg::new(Arc::new(key_pair), best_hash),
            )
            .await
            .map_err(|e| format!("Public key authentication error: {}", e))?;

        if !auth_res.success() {
            return Err("SSH public key authentication rejected by server".to_string());
        }
    } else {
        let pw = password.unwrap_or("");
        let auth_res = session
            .authenticate_password(user, pw)
            .await
            .map_err(|e| format!("Password authentication error: {}", e))?;

        if !auth_res.success() {
            // Fallback to SSH keyboard-interactive (required by macOS Remote Login, PAM, RHEL, SUSE, etc.)
            let mut kbd_ok = false;
            if let Ok(mut resp) = session.authenticate_keyboard_interactive_start(user, None).await {
                for _ in 0..3 {
                    match resp {
                        client::KeyboardInteractiveAuthResponse::Success => {
                            kbd_ok = true;
                            break;
                        }
                        client::KeyboardInteractiveAuthResponse::InfoRequest { prompts, .. } => {
                            let answers = vec![pw.to_string(); prompts.len()];
                            match session.authenticate_keyboard_interactive_respond(answers).await {
                                Ok(next) => resp = next,
                                Err(_) => break,
                            }
                        }
                        client::KeyboardInteractiveAuthResponse::Failure { .. } => break,
                    }
                }
            }
            if !kbd_ok {
                return Err("SSH password authentication rejected (invalid username or password)".to_string());
            }
        }
    }

    Ok(session)
}

#[cfg(target_os = "macos")]
async fn open_macos_ssh_pty_fallback(
    app: AppHandle,
    state: State<'_, TerminalState>,
    config: TerminalOpenConfig,
    rows: u16,
    cols: u16,
) -> Result<(), String> {
    use std::os::unix::fs::PermissionsExt;

    let (clean_host, actual_port) = normalize_host_port(
        config.host.as_deref().unwrap_or(""),
        config.port.unwrap_or(22),
    );
    let user = config
        .username
        .as_deref()
        .map(str::trim)
        .filter(|u| !u.is_empty())
        .unwrap_or("root");

    let pty_system = native_pty_system();
    let pair = pty_system
        .openpty(PtySize {
            rows,
            cols,
            pixel_width: 0,
            pixel_height: 0,
        })
        .map_err(|e| e.to_string())?;

    let mut cmd = CommandBuilder::new("/usr/bin/ssh");
    cmd.env("TERM", "xterm-256color");
    cmd.arg("-p");
    cmd.arg(actual_port.to_string());
    cmd.arg("-o");
    cmd.arg("StrictHostKeyChecking=accept-new");

    let mut temp_key_path: Option<std::path::PathBuf> = None;
    if let Some(key_pem) = config.private_key.as_deref().filter(|k| !k.trim().is_empty()) {
        let key_file = std::env::temp_dir().join(format!("opentermius_key_{}", config.session_id));
        if std::fs::write(&key_file, format!("{}\n", key_pem.trim())).is_ok() {
            let _ = std::fs::set_permissions(&key_file, std::fs::Permissions::from_mode(0o600));
            cmd.arg("-i");
            cmd.arg(key_file.to_string_lossy().to_string());
            cmd.arg("-o");
            cmd.arg("IdentitiesOnly=yes");
            temp_key_path = Some(key_file);
        }
    }
    cmd.arg(format!("{}@{}", user, clean_host));

    let child = pair.slave.spawn_command(cmd).map_err(|e| e.to_string())?;
    drop(pair.slave);

    let reader = pair.master.try_clone_reader().map_err(|e| e.to_string())?;
    let writer = Arc::new(Mutex::new(
        pair.master.take_writer().map_err(|e| e.to_string())?,
    ));

    let session_id_clone = config.session_id.clone();
    let app_clone = app.clone();
    let sessions_arc = state.sessions.clone();

    {
        let mut sessions = state.sessions.lock().await;
        sessions.insert(
            config.session_id,
            SessionHandle::LocalPty {
                writer,
                master: Arc::new(Mutex::new(pair.master)),
                child: Arc::new(Mutex::new(child)),
            },
        );
    }

    std::thread::spawn(move || {
        let mut buf = [0u8; 4096];
        let mut utf8_pending = Vec::new();
        let mut r = reader;
        let event_name = format!("terminal-data:{}", session_id_clone);
        loop {
            match r.read(&mut buf) {
                Ok(0) => break,
                Ok(n) => {
                    let chunk = drain_utf8_chunk(&mut utf8_pending, &buf[..n]);
                    if !chunk.is_empty() && app_clone.emit(&event_name, chunk).is_err() {
                        break;
                    }
                }
                Err(_) => break,
            }
        }
        if let Some(p) = temp_key_path {
            let _ = std::fs::remove_file(p);
        }
        tauri::async_runtime::block_on(async {
            sessions_arc.lock().await.remove(&session_id_clone);
        });
        let exit_event = format!("terminal-exit:{}", session_id_clone);
        let _ = app_clone.emit(&exit_event, ());
    });

    Ok(())
}

#[tauri::command]
async fn terminal_open(
    app: AppHandle,
    state: State<'_, TerminalState>,
    config: TerminalOpenConfig,
) -> Result<(), String> {
    let rows = config.rows.unwrap_or(24).max(1);
    let cols = config.cols.unwrap_or(80).max(1);
    let host_target = config.host.as_deref().unwrap_or("").trim().to_string();

    if !host_target.is_empty() {
        let port = config.port.unwrap_or(22);
        let username = config.username.as_deref().unwrap_or("root");
        match establish_ssh_session(
            &host_target,
            port,
            username,
            config.password.as_deref(),
            config.private_key.as_deref(),
            config.passphrase.as_deref(),
        )
        .await
        {
            Ok(session) => {
                let channel = session
                    .channel_open_session()
                    .await
                    .map_err(|e| format!("Failed to open SSH channel: {}", e))?;

                channel
                    .request_pty(true, "xterm-256color", cols as u32, rows as u32, 0, 0, &[])
                    .await
                    .map_err(|e| format!("Failed to request PTY: {}", e))?;

                channel
                    .request_shell(true)
                    .await
                    .map_err(|e| format!("Failed to start remote shell: {}", e))?;

                let (mut read_half, write_half) = channel.split();
                let session_id_clone = config.session_id.clone();
                let app_clone = app.clone();
                let sessions_arc = state.sessions.clone();

                tauri::async_runtime::spawn(async move {
                    let event_name = format!("terminal-data:{}", session_id_clone);
                    let mut utf8_pending = Vec::new();
                    while let Some(msg) = read_half.wait().await {
                        match msg {
                            ChannelMsg::Data { ref data } => {
                                let chunk = drain_utf8_chunk(&mut utf8_pending, data);
                                if !chunk.is_empty() && app_clone.emit(&event_name, chunk).is_err() {
                                    break;
                                }
                            }
                            ChannelMsg::ExtendedData { ref data, .. } => {
                                let chunk = drain_utf8_chunk(&mut utf8_pending, data);
                                if !chunk.is_empty() && app_clone.emit(&event_name, chunk).is_err() {
                                    break;
                                }
                            }
                            ChannelMsg::Eof | ChannelMsg::Close => break,
                            _ => {}
                        }
                    }
                    sessions_arc.lock().await.remove(&session_id_clone);
                    let exit_event = format!("terminal-exit:{}", session_id_clone);
                    let _ = app_clone.emit(&exit_event, ());
                });

                let mut sessions = state.sessions.lock().await;
                sessions.insert(
                    config.session_id,
                    SessionHandle::RemoteSsh {
                        write_half: Arc::new(tokio::sync::Mutex::new(write_half)),
                        session: Arc::new(tokio::sync::Mutex::new(session)),
                    },
                );
                return Ok(());
            }
            Err(err) => {
                #[cfg(target_os = "macos")]
                if err.contains("os error 65") || err.to_lowercase().contains("no route to host") {
                    return open_macos_ssh_pty_fallback(app, state, config, rows, cols).await;
                }
                return Err(err);
            }
        }
    }

    // Local Shell via native PTY
    let pty_system = native_pty_system();
    let size = PtySize {
        rows,
        cols,
        pixel_width: 0,
        pixel_height: 0,
    };

    let pair = pty_system.openpty(size).map_err(|e| e.to_string())?;

    #[cfg(target_os = "windows")]
    let shell = std::env::var("COMSPEC")
        .ok()
        .filter(|s| std::path::Path::new(s).exists())
        .unwrap_or_else(|| "powershell.exe".to_string());

    #[cfg(not(target_os = "windows"))]
    let shell = {
        #[cfg(target_os = "android")]
        let default_shell = "/system/bin/sh";
        #[cfg(not(target_os = "android"))]
        let default_shell = "/bin/zsh";

        std::env::var("SHELL")
            .ok()
            .filter(|s| std::path::Path::new(s).exists())
            .unwrap_or_else(|| {
                if std::path::Path::new(default_shell).exists() {
                    default_shell.to_string()
                } else {
                    "/bin/sh".to_string()
                }
            })
    };

    let mut cmd = CommandBuilder::new(shell);
    cmd.env("TERM", "xterm-256color");

    let child = pair.slave.spawn_command(cmd).map_err(|e| e.to_string())?;
    drop(pair.slave);

    let reader = pair.master.try_clone_reader().map_err(|e| e.to_string())?;
    let writer = Arc::new(Mutex::new(
        pair.master.take_writer().map_err(|e| e.to_string())?,
    ));

    let session_id_clone = config.session_id.clone();
    let app_clone = app.clone();
    let sessions_arc = state.sessions.clone();

    {
        let mut sessions = state.sessions.lock().await;
        sessions.insert(
            config.session_id,
            SessionHandle::LocalPty {
                writer,
                master: Arc::new(Mutex::new(pair.master)),
                child: Arc::new(Mutex::new(child)),
            },
        );
    }

    std::thread::spawn(move || {
        let mut buf = [0u8; 4096];
        let mut utf8_pending = Vec::new();
        let mut r = reader;
        let event_name = format!("terminal-data:{}", session_id_clone);
        loop {
            match r.read(&mut buf) {
                Ok(0) => break,
                Ok(n) => {
                    let chunk = drain_utf8_chunk(&mut utf8_pending, &buf[..n]);
                    if !chunk.is_empty() && app_clone.emit(&event_name, chunk).is_err() {
                        break;
                    }
                }
                Err(_) => break,
            }
        }
        tauri::async_runtime::block_on(async {
            sessions_arc.lock().await.remove(&session_id_clone);
        });
        let exit_event = format!("terminal-exit:{}", session_id_clone);
        let _ = app_clone.emit(&exit_event, ());
    });

    Ok(())
}

#[tauri::command]
async fn terminal_write(
    state: State<'_, TerminalState>,
    session_id: String,
    data: String,
) -> Result<(), String> {
    let target = {
        let sessions = state.sessions.lock().await;
        match sessions.get(&session_id) {
            Some(SessionHandle::LocalPty { writer, .. }) => Ok((Some(writer.clone()), None)),
            Some(SessionHandle::RemoteSsh { write_half, .. }) => Ok((None, Some(write_half.clone()))),
            None => Err("Session not found".to_string()),
        }
    }?;

    if let Some(writer) = target.0 {
        tokio::task::spawn_blocking(move || {
            let mut w = writer.lock().map_err(|_| "Terminal writer lock poisoned".to_string())?;
            w.write_all(data.as_bytes()).map_err(|e| e.to_string())?;
            w.flush().map_err(|e| e.to_string())
        })
        .await
        .map_err(|e| e.to_string())??;
        return Ok(());
    }

    if let Some(write_half) = target.1 {
        let wh = write_half.lock().await;
        wh.data(data.as_bytes()).await.map_err(|e| e.to_string())?;
    }
    Ok(())
}

#[tauri::command]
async fn terminal_resize(
    state: State<'_, TerminalState>,
    session_id: String,
    cols: u16,
    rows: u16,
) -> Result<(), String> {
    let target = {
        let sessions = state.sessions.lock().await;
        match sessions.get(&session_id) {
            Some(SessionHandle::LocalPty { master, .. }) => Ok((Some(master.clone()), None)),
            Some(SessionHandle::RemoteSsh { write_half, .. }) => Ok((None, Some(write_half.clone()))),
            None => Err("Session not found".to_string()),
        }
    }?;

    if let Some(master) = target.0 {
        let m = master.lock().map_err(|_| "Terminal master lock poisoned".to_string())?;
        m.resize(PtySize {
            rows,
            cols,
            pixel_width: 0,
            pixel_height: 0,
        })
        .map_err(|e| e.to_string())?;
        return Ok(());
    }

    if let Some(write_half) = target.1 {
        let wh = write_half.lock().await;
        wh.window_change(cols as u32, rows as u32, 0, 0)
            .await
            .map_err(|e| e.to_string())?;
    }
    Ok(())
}

#[tauri::command]
async fn terminal_close(state: State<'_, TerminalState>, session_id: String) -> Result<(), String> {
    let removed = {
        let mut sessions = state.sessions.lock().await;
        sessions.remove(&session_id)
    };
    if let Some(handle) = removed {
        match handle {
            SessionHandle::LocalPty { child, .. } => {
                let _ = tokio::task::spawn_blocking(move || {
                    if let Ok(mut c) = child.lock() {
                        let _ = c.kill();
                        let _ = c.wait();
                    }
                })
                .await;
            }
            SessionHandle::RemoteSsh {
                write_half,
                session,
            } => {
                let wh = write_half.lock().await;
                let _ = wh.close().await;
                drop(wh);
                let s = session.lock().await;
                let _ = s
                    .disconnect(Disconnect::ByApplication, "Closed by user", "en")
                    .await;
            }
        }
    }
    Ok(())
}

// ============================================================================
// Native Remote SFTP Commands (via russh + russh-sftp)
// ============================================================================

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SftpConnectionConfig {
    pub address: String,
    pub port: Option<u16>,
    pub username: String,
    pub password: Option<String>,
    pub private_key: Option<String>,
    pub passphrase: Option<String>,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct RemoteSftpItem {
    pub name: String,
    pub path: String,
    pub size: u64,
    pub is_dir: bool,
    pub mod_time: String,
    pub permissions: String,
}

#[derive(Serialize)]
pub struct RemoteSftpListResult {
    pub path: String,
    pub items: Vec<RemoteSftpItem>,
}

struct PooledSftp {
    _ssh: client::Handle<SshClientHandler>,
    sftp: Arc<SftpSession>,
}

#[derive(Default)]
pub struct SftpPoolState {
    sessions: Arc<tokio::sync::Mutex<HashMap<String, PooledSftp>>>,
}

fn sftp_pool_key(cfg: &SftpConnectionConfig) -> String {
    let (clean_host, actual_port) = normalize_host_port(&cfg.address, cfg.port.unwrap_or(22));
    let user = if cfg.username.trim().is_empty() {
        "root"
    } else {
        cfg.username.trim()
    };
    format!("{}:{}:{}", clean_host, actual_port, user)
}

async fn open_sftp_session(
    cfg: &SftpConnectionConfig,
) -> Result<(client::Handle<SshClientHandler>, SftpSession), String> {
    let session = establish_ssh_session(
        cfg.address.trim(),
        cfg.port.unwrap_or(22),
        cfg.username.trim(),
        cfg.password.as_deref(),
        cfg.private_key.as_deref(),
        cfg.passphrase.as_deref(),
    )
    .await?;

    let channel = session
        .channel_open_session()
        .await
        .map_err(|e| format!("Failed to open SSH channel for SFTP: {}", e))?;

    channel
        .request_subsystem(true, "sftp")
        .await
        .map_err(|e| format!("Failed to start SFTP subsystem: {}", e))?;

    let sftp = SftpSession::new(channel.into_stream())
        .await
        .map_err(|e| format!("Failed to initialize SFTP session: {}", e))?;

    Ok((session, sftp))
}

async fn get_pooled_sftp(
    pool: &SftpPoolState,
    cfg: &SftpConnectionConfig,
    force_new: bool,
) -> Result<Arc<SftpSession>, String> {
    let key = sftp_pool_key(cfg);
    if !force_new {
        let sessions = pool.sessions.lock().await;
        if let Some(existing) = sessions.get(&key) {
            return Ok(existing.sftp.clone());
        }
    }
    let (ssh, sftp) = open_sftp_session(cfg).await?;
    let arc_sftp = Arc::new(sftp);
    let mut sessions = pool.sessions.lock().await;
    sessions.insert(
        key,
        PooledSftp {
            _ssh: ssh,
            sftp: arc_sftp.clone(),
        },
    );
    Ok(arc_sftp)
}

async fn evict_pooled_sftp(pool: &SftpPoolState, cfg: &SftpConnectionConfig) {
    let key = sftp_pool_key(cfg);
    pool.sessions.lock().await.remove(&key);
}

#[tauri::command]
async fn sftp_list(
    pool: State<'_, SftpPoolState>,
    config: SftpConnectionConfig,
    path: String,
) -> Result<RemoteSftpListResult, String> {
    let mut sftp = get_pooled_sftp(&pool, &config, false).await?;
    let target = if path.trim().is_empty() {
        "."
    } else {
        path.trim()
    };

    let mut canonical = sftp
        .canonicalize(target)
        .await
        .unwrap_or_else(|_| target.to_string());

    let read_dir = match sftp.read_dir(&canonical).await {
        Ok(rd) => rd,
        Err(_) => {
            sftp = get_pooled_sftp(&pool, &config, true).await?;
            canonical = sftp
                .canonicalize(target)
                .await
                .unwrap_or_else(|_| target.to_string());
            sftp.read_dir(&canonical)
                .await
                .map_err(|e| format!("Failed to read remote directory '{}': {}", canonical, e))?
        }
    };

    let mut items = Vec::new();
    for entry in read_dir {
        let name = entry.file_name();
        if name == "." || name == ".." {
            continue;
        }
        let meta = entry.metadata();
        let is_dir = meta.is_dir();
        let size = if is_dir { 0 } else { meta.len() };
        let mod_time = meta.mtime.unwrap_or(0).to_string();
        let permissions = format!(
            "{}{}",
            if is_dir { "d" } else { "-" },
            meta.permissions()
        );

        let full_path = if canonical == "/" {
            format!("/{}", name)
        } else {
            format!("{}/{}", canonical.trim_end_matches('/'), name)
        };

        items.push(RemoteSftpItem {
            name,
            path: full_path,
            size,
            is_dir,
            mod_time,
            permissions,
        });
    }

    items.sort_by(|a, b| {
        b.is_dir
            .cmp(&a.is_dir)
            .then_with(|| a.name.to_lowercase().cmp(&b.name.to_lowercase()))
    });

    Ok(RemoteSftpListResult {
        path: canonical,
        items,
    })
}

#[tauri::command]
async fn sftp_mkdir(
    pool: State<'_, SftpPoolState>,
    config: SftpConnectionConfig,
    path: String,
) -> Result<(), String> {
    let sftp = get_pooled_sftp(&pool, &config, false).await?;
    if sftp.create_dir(&path).await.is_err() {
        let sftp = get_pooled_sftp(&pool, &config, true).await?;
        sftp.create_dir(&path)
            .await
            .map_err(|e| format!("Failed to create remote directory '{}': {}", path, e))?;
    }
    Ok(())
}

#[tauri::command]
async fn sftp_delete(
    pool: State<'_, SftpPoolState>,
    config: SftpConnectionConfig,
    path: String,
) -> Result<(), String> {
    let mut sftp = get_pooled_sftp(&pool, &config, false).await?;
    let meta = match sftp.metadata(&path).await {
        Ok(m) => Ok(m),
        Err(_) => {
            sftp = get_pooled_sftp(&pool, &config, true).await?;
            sftp.metadata(&path).await
        }
    };
    let is_dir = meta.map(|m| m.is_dir()).unwrap_or(false);

    if is_dir {
        sftp.remove_dir(&path)
            .await
            .map_err(|e| format!("Failed to remove remote directory '{}': {}", path, e))?;
    } else {
        sftp.remove_file(&path)
            .await
            .map_err(|e| format!("Failed to delete remote file '{}': {}", path, e))?;
    }
    Ok(())
}

const MAX_TRANSFER_BYTES: u64 = 100 * 1024 * 1024; // 100 MB safety cap

#[tauri::command]
async fn sftp_read_bytes(
    pool: State<'_, SftpPoolState>,
    config: SftpConnectionConfig,
    path: String,
) -> Result<Vec<u8>, String> {
    let mut sftp = get_pooled_sftp(&pool, &config, false).await?;
    let meta = match sftp.metadata(&path).await {
        Ok(m) => m,
        Err(_) => {
            sftp = get_pooled_sftp(&pool, &config, true).await?;
            sftp.metadata(&path)
                .await
                .map_err(|e| format!("Failed to stat '{}': {}", path, e))?
        }
    };
    if meta.len() > MAX_TRANSFER_BYTES {
        return Err(format!(
            "File '{}' exceeds 100 MB in-memory transfer limit",
            path
        ));
    }
    sftp.read(&path)
        .await
        .map_err(|e| format!("Failed to download '{}': {}", path, e))
}

#[tauri::command]
async fn sftp_write_bytes(
    pool: State<'_, SftpPoolState>,
    config: SftpConnectionConfig,
    path: String,
    data: Vec<u8>,
) -> Result<(), String> {
    let mut sftp = get_pooled_sftp(&pool, &config, false).await?;
    let mut file = match sftp.create(&path).await {
        Ok(f) => f,
        Err(_) => {
            sftp = get_pooled_sftp(&pool, &config, true).await?;
            sftp.create(&path)
                .await
                .map_err(|e| format!("Failed to create remote file '{}': {}", path, e))?
        }
    };
    if let Err(e) = file.write_all(&data).await {
        evict_pooled_sftp(&pool, &config).await;
        return Err(format!("Failed to write remote file '{}': {}", path, e));
    }
    file.flush()
        .await
        .map_err(|e| format!("Failed to flush remote file '{}': {}", path, e))?;
    let _ = file.shutdown().await;
    Ok(())
}

#[tauri::command]
async fn sftp_rename(
    pool: State<'_, SftpPoolState>,
    config: SftpConnectionConfig,
    old_path: String,
    new_path: String,
) -> Result<(), String> {
    let sftp = get_pooled_sftp(&pool, &config, false).await?;
    if sftp.rename(&old_path, &new_path).await.is_err() {
        let sftp = get_pooled_sftp(&pool, &config, true).await?;
        sftp.rename(&old_path, &new_path)
            .await
            .map_err(|e| format!("Failed to rename '{}': {}", old_path, e))?;
    }
    Ok(())
}

// ============================================================================
// SSH Keypair Generation & Native Port Forwarding (Local -L & Dynamic SOCKS5)
// ============================================================================

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct GeneratedKeyPair {
    pub public_key: String,
    pub private_key: String,
}

#[tauri::command]
async fn ssh_generate_keypair(
    key_type: String,
    passphrase: Option<String>,
) -> Result<GeneratedKeyPair, String> {
    tokio::task::spawn_blocking(move || {
        let mut rng = rand::rng();
        let priv_key = if key_type.to_uppercase().starts_with("RSA") {
            let rsa = ssh_key::private::RsaKeypair::random(&mut rng, 4096)
                .map_err(|e| format!("Failed to generate RSA-4096 key: {}", e))?;
            PrivateKey::from(rsa)
        } else {
            PrivateKey::random(&mut rng, ssh_key::Algorithm::Ed25519)
                .map_err(|e| format!("Failed to generate Ed25519 key: {}", e))?
        };

        let pub_openssh = priv_key
            .public_key()
            .to_openssh()
            .map_err(|e| format!("Failed to encode public key: {}", e))?;

        let final_key = if let Some(pass) = passphrase.filter(|p| !p.is_empty()) {
            priv_key
                .encrypt(&mut rng, pass)
                .map_err(|e| format!("Failed to encrypt private key: {}", e))?
        } else {
            priv_key
        };

        let priv_pem = final_key
            .to_openssh(ssh_key::LineEnding::LF)
            .map_err(|e| format!("Failed to encode private key: {}", e))?
            .to_string();

        Ok(GeneratedKeyPair {
            public_key: format!("{} opentermius", pub_openssh),
            private_key: priv_pem,
        })
    })
    .await
    .map_err(|e| e.to_string())?
}

#[derive(Default)]
pub struct TunnelState {
    tunnels: Arc<tokio::sync::Mutex<HashMap<String, tauri::async_runtime::JoinHandle<()>>>>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct TunnelStartConfig {
    pub tunnel_id: String,
    pub tunnel_type: String,
    pub local_port: u16,
    pub remote_host: Option<String>,
    pub remote_port: Option<u16>,
    pub address: String,
    pub port: Option<u16>,
    pub username: String,
    pub password: Option<String>,
    pub private_key: Option<String>,
    pub passphrase: Option<String>,
}

async fn handle_socks5_client(
    mut stream: tokio::net::TcpStream,
    ssh: Arc<tokio::sync::Mutex<client::Handle<SshClientHandler>>>,
    peer_port: u32,
) -> Result<(), String> {
    let mut head = [0u8; 2];
    stream.read_exact(&mut head).await.map_err(|e| e.to_string())?;
    if head[0] != 0x05 {
        return Err("Unsupported SOCKS version".to_string());
    }
    let nmethods = head[1] as usize;
    let mut methods = vec![0u8; nmethods];
    stream.read_exact(&mut methods).await.map_err(|e| e.to_string())?;
    if !methods.contains(&0x00) {
        let _ = stream.write_all(&[0x05, 0xFF]).await;
        return Err("No supported SOCKS5 auth method".to_string());
    }
    stream.write_all(&[0x05, 0x00]).await.map_err(|e| e.to_string())?;

    let mut req = [0u8; 4];
    stream.read_exact(&mut req).await.map_err(|e| e.to_string())?;
    if req[0] != 0x05 || req[1] != 0x01 {
        let _ = stream.write_all(&[0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0]).await;
        return Err("Only SOCKS5 CONNECT is supported".to_string());
    }

    let target_host = match req[3] {
        0x01 => {
            let mut ip = [0u8; 4];
            stream.read_exact(&mut ip).await.map_err(|e| e.to_string())?;
            std::net::Ipv4Addr::from(ip).to_string()
        }
        0x03 => {
            let mut len = [0u8; 1];
            stream.read_exact(&mut len).await.map_err(|e| e.to_string())?;
            let mut domain = vec![0u8; len[0] as usize];
            stream.read_exact(&mut domain).await.map_err(|e| e.to_string())?;
            String::from_utf8_lossy(&domain).to_string()
        }
        0x04 => {
            let mut ip = [0u8; 16];
            stream.read_exact(&mut ip).await.map_err(|e| e.to_string())?;
            std::net::Ipv6Addr::from(ip).to_string()
        }
        _ => {
            let _ = stream.write_all(&[0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0]).await;
            return Err("Unsupported address type".to_string());
        }
    };

    let mut port_buf = [0u8; 2];
    stream.read_exact(&mut port_buf).await.map_err(|e| e.to_string())?;
    let target_port = u16::from_be_bytes(port_buf) as u32;

    let channel = {
        let s = ssh.lock().await;
        s.channel_open_direct_tcpip(target_host, target_port, "127.0.0.1", peer_port)
            .await
    };
    let channel = match channel {
        Ok(ch) => ch,
        Err(e) => {
            let _ = stream.write_all(&[0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0]).await;
            return Err(e.to_string());
        }
    };

    stream
        .write_all(&[0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0])
        .await
        .map_err(|e| e.to_string())?;
    let mut ch_stream = channel.into_stream();
    let _ = tokio::io::copy_bidirectional(&mut stream, &mut ch_stream).await;
    Ok(())
}

#[tauri::command]
async fn tunnel_start(
    app: AppHandle,
    state: State<'_, TunnelState>,
    config: TunnelStartConfig,
) -> Result<(), String> {
    if let Some(prev) = state.tunnels.lock().await.remove(&config.tunnel_id) {
        prev.abort();
    }

    let listener = TcpListener::bind(("127.0.0.1", config.local_port))
        .await
        .map_err(|e| format!("Failed to bind local port {}: {}", config.local_port, e))?;

    let session = establish_ssh_session(
        config.address.trim(),
        config.port.unwrap_or(22),
        config.username.trim(),
        config.password.as_deref(),
        config.private_key.as_deref(),
        config.passphrase.as_deref(),
    )
    .await?;

    let ssh = Arc::new(tokio::sync::Mutex::new(session));
    let tunnel_id = config.tunnel_id.clone();
    let tunnel_type = config.tunnel_type.clone();
    let remote_host = config
        .remote_host
        .clone()
        .unwrap_or_else(|| "127.0.0.1".to_string());
    let remote_port = config.remote_port.unwrap_or(5432) as u32;
    let tunnels_arc = state.tunnels.clone();

    let handle = tauri::async_runtime::spawn(async move {
        loop {
            let Ok((mut tcp_stream, peer_addr)) = listener.accept().await else {
                break;
            };
            let ssh_clone = ssh.clone();
            let t_type = tunnel_type.clone();
            let r_host = remote_host.clone();
            let peer_port = peer_addr.port() as u32;

            tauri::async_runtime::spawn(async move {
                if t_type == "dynamic" {
                    let _ = handle_socks5_client(tcp_stream, ssh_clone, peer_port).await;
                } else {
                    let ch = {
                        let s = ssh_clone.lock().await;
                        s.channel_open_direct_tcpip(r_host, remote_port, "127.0.0.1", peer_port)
                            .await
                    };
                    if let Ok(channel) = ch {
                        let mut ch_stream = channel.into_stream();
                        let _ = tokio::io::copy_bidirectional(&mut tcp_stream, &mut ch_stream).await;
                    }
                }
            });
        }
        tunnels_arc.lock().await.remove(&tunnel_id);
        let _ = app.emit(&format!("tunnel-closed:{}", tunnel_id), ());
    });

    state.tunnels.lock().await.insert(config.tunnel_id, handle);
    Ok(())
}

#[tauri::command]
async fn tunnel_stop(state: State<'_, TunnelState>, tunnel_id: String) -> Result<(), String> {
    if let Some(handle) = state.tunnels.lock().await.remove(&tunnel_id) {
        handle.abort();
    }
    Ok(())
}

// ============================================================================
// Local Filesystem Commands
// ============================================================================

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct LocalFsItem {
    pub name: String,
    pub path: String,
    pub size: u64,
    pub is_dir: bool,
    pub mod_time: String,
    pub mode: String,
}

#[derive(Serialize)]
pub struct LocalFsListResult {
    pub path: String,
    pub items: Vec<LocalFsItem>,
}

#[tauri::command]
async fn local_fs_list(path: String) -> Result<LocalFsListResult, String> {
    tokio::task::spawn_blocking(move || {
        let target = if path.trim().is_empty() {
            #[cfg(target_os = "android")]
            let fallback = PathBuf::from("/sdcard");
            #[cfg(not(target_os = "android"))]
            let fallback = PathBuf::from("/");

            user_home_dir().unwrap_or_else(|| {
                if fallback.exists() {
                    fallback
                } else {
                    PathBuf::from("/")
                }
            })
        } else {
            PathBuf::from(path.trim())
        };

        let canonical = std::fs::canonicalize(&target).unwrap_or(target);
        let read_dir = std::fs::read_dir(&canonical).map_err(|e| e.to_string())?;

        let mut items = Vec::new();
        for entry in read_dir.flatten() {
            let name = entry.file_name().to_string_lossy().to_string();
            let entry_path = entry.path();
            let meta = match entry.metadata() {
                Ok(m) => m,
                Err(_) => continue,
            };
            let is_dir = meta.is_dir();
            let size = if is_dir { 0 } else { meta.len() };
            let mod_secs = meta
                .modified()
                .ok()
                .and_then(|t| t.duration_since(UNIX_EPOCH).ok())
                .map(|d| d.as_secs())
                .unwrap_or(0);
            #[cfg(unix)]
            let mode = {
                use std::os::unix::fs::PermissionsExt;
                format!("{:o}", meta.permissions().mode() & 0o777)
            };
            #[cfg(not(unix))]
            let mode = String::from("644");

            items.push(LocalFsItem {
                name,
                path: entry_path.to_string_lossy().to_string(),
                size,
                is_dir,
                mod_time: mod_secs.to_string(),
                mode,
            });
        }

        items.sort_by(|a, b| {
            b.is_dir
                .cmp(&a.is_dir)
                .then_with(|| a.name.to_lowercase().cmp(&b.name.to_lowercase()))
        });

        Ok(LocalFsListResult {
            path: canonical.to_string_lossy().to_string(),
            items,
        })
    })
    .await
    .map_err(|e| e.to_string())?
}

#[tauri::command]
async fn local_fs_mkdir(path: String) -> Result<(), String> {
    tokio::task::spawn_blocking(move || std::fs::create_dir_all(path).map_err(|e| e.to_string()))
        .await
        .map_err(|e| e.to_string())?
}

#[tauri::command]
async fn local_fs_delete(path: String) -> Result<(), String> {
    tokio::task::spawn_blocking(move || {
        let p = PathBuf::from(&path);
        if p.is_dir() {
            std::fs::remove_dir_all(p).map_err(|e| e.to_string())
        } else {
            std::fs::remove_file(p).map_err(|e| e.to_string())
        }
    })
    .await
    .map_err(|e| e.to_string())?
}

#[tauri::command]
async fn local_fs_read_bytes(path: String) -> Result<Vec<u8>, String> {
    tokio::task::spawn_blocking(move || {
        let meta = std::fs::metadata(&path).map_err(|e| e.to_string())?;
        if meta.len() > MAX_TRANSFER_BYTES {
            return Err(format!(
                "File '{}' exceeds 100 MB in-memory transfer limit",
                path
            ));
        }
        std::fs::read(path).map_err(|e| e.to_string())
    })
    .await
    .map_err(|e| e.to_string())?
}

#[tauri::command]
async fn local_fs_write_bytes(path: String, data: Vec<u8>) -> Result<(), String> {
    tokio::task::spawn_blocking(move || std::fs::write(path, data).map_err(|e| e.to_string()))
        .await
        .map_err(|e| e.to_string())?
}

#[tauri::command]
async fn local_fs_rename(old_path: String, new_path: String) -> Result<(), String> {
    tokio::task::spawn_blocking(move || std::fs::rename(old_path, new_path).map_err(|e| e.to_string()))
        .await
        .map_err(|e| e.to_string())?
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    #[allow(unused_mut)]
    let mut builder = tauri::Builder::default()
        .manage(TerminalState::default())
        .manage(SftpPoolState::default())
        .manage(TunnelState::default())
        .plugin(
            tauri_plugin_log::Builder::default()
                .level(log::LevelFilter::Info)
                .build(),
        );

    #[cfg(desktop)]
    {
        builder = builder
            .plugin(tauri_plugin_updater::Builder::new().build())
            .plugin(tauri_plugin_process::init());
    }

    builder
        .invoke_handler(tauri::generate_handler![
            terminal_open,
            terminal_write,
            terminal_resize,
            terminal_close,
            ssh_trust_host_key,
            ssh_generate_keypair,
            tunnel_start,
            tunnel_stop,
            sftp_list,
            sftp_mkdir,
            sftp_delete,
            sftp_rename,
            sftp_read_bytes,
            sftp_write_bytes,
            local_fs_list,
            local_fs_mkdir,
            local_fs_delete,
            local_fs_rename,
            local_fs_read_bytes,
            local_fs_write_bytes
        ])
        .run(tauri::generate_context!())
        .expect("error while building tauri application");
}
