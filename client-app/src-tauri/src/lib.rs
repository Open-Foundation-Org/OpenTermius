use std::collections::HashMap;
use std::io::{Read, Write};
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex, OnceLock};
use std::time::{Duration, UNIX_EPOCH};

use portable_pty::{native_pty_system, CommandBuilder, MasterPty, PtySize};
use russh::keys::{decode_secret_key, PrivateKeyWithHashAlg, PublicKeyOrCertificate};
use russh::{client, ChannelMsg, ChannelWriteHalf, Disconnect};
use russh_sftp::client::SftpSession;
use serde::{Deserialize, Serialize};
use tauri::{AppHandle, Emitter, State};
use tokio::io::AsyncWriteExt;

static KNOWN_HOSTS: OnceLock<Mutex<HashMap<String, String>>> = OnceLock::new();

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

fn verify_or_record_host_key(host_port: &str, key_repr: &str) -> bool {
    let Ok(mut map) = known_hosts_map().lock() else {
        return true;
    };

    if let Some(existing) = map.get(host_port) {
        return existing == key_repr;
    }

    map.insert(host_port.to_string(), key_repr.to_string());
    persist_known_hosts(&map);
    true
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
    key_mismatch: Arc<AtomicBool>,
}

impl client::Handler for SshClientHandler {
    type Error = russh::Error;

    async fn check_server_key(
        &mut self,
        server_public_key: &PublicKeyOrCertificate,
    ) -> Result<bool, Self::Error> {
        let key_repr = format!("{:?}", server_public_key);
        let ok = verify_or_record_host_key(&self.host_port, &key_repr);
        if !ok {
            self.key_mismatch.store(true, Ordering::SeqCst);
        }
        Ok(ok)
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
    if let Ok(mut map) = known_hosts_map().lock() {
        map.remove(&key);
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
    let key_mismatch = Arc::new(AtomicBool::new(false));
    let handler = SshClientHandler {
        host_port: format!("{}:{}", clean_host, actual_port),
        key_mismatch: key_mismatch.clone(),
    };
    let mut session = tokio::time::timeout(
        Duration::from_secs(15),
        client::connect(config, addr, handler),
    )
    .await
    .map_err(|_| format!("Connection timed out to {}:{}", clean_host, actual_port))?
    .map_err(|e| {
        if key_mismatch.load(Ordering::SeqCst) {
            format!(
                "HOST_KEY_MISMATCH:{}:{} — Server host key changed. Click 'Trust New Key' if this server was reinstalled.",
                clean_host, actual_port
            )
        } else {
            format!("SSH connection failed to {}:{}: {}", clean_host, actual_port, e)
        }
    })?;

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
        let session = establish_ssh_session(
            &host_target,
            port,
            username,
            config.password.as_deref(),
            config.private_key.as_deref(),
            config.passphrase.as_deref(),
        )
        .await?;

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
        let exit_event = format!("terminal-exit:{}", session_id_clone);
        let _ = app_clone.emit(&exit_event, ());
    });

    let mut sessions = state.sessions.lock().await;
    sessions.insert(
        config.session_id,
        SessionHandle::LocalPty {
            writer,
            master: Arc::new(Mutex::new(pair.master)),
            child: Arc::new(Mutex::new(child)),
        },
    );

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

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    #[allow(unused_mut)]
    let mut builder = tauri::Builder::default()
        .manage(TerminalState::default())
        .manage(SftpPoolState::default())
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
            sftp_list,
            sftp_mkdir,
            sftp_delete,
            sftp_read_bytes,
            sftp_write_bytes,
            local_fs_list,
            local_fs_mkdir,
            local_fs_delete,
            local_fs_read_bytes,
            local_fs_write_bytes
        ])
        .run(tauri::generate_context!())
        .expect("error while building tauri application");
}
