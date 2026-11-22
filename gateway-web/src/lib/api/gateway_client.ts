// OpenTermius Gateway Admin API Client

export interface GatewayUser {
  id: string;
  email: string;
  name: string;
  role?: string;
  created_at: string;
}

export interface GatewayVault {
  id: string;
  owner_id: string;
  name: string;
  type: 'personal' | 'team';
  created_at: string;
  updated_at: string;
}

export interface GatewayMember {
  id: string;
  vault_id: string;
  user_id: string;
  email?: string;
  role: 'owner' | 'admin' | 'member';
  created_at: string;
}

export interface GatewaySessionLog {
  id: string;
  user_id: string;
  host_id: string;
  session_type: string;
  started_at: string;
  ended_at?: string;
  duration_secs?: number;
}

export interface GatewayActiveSession {
  session_id: string;
  vault_id: string;
  vault_name: string;
  host_label: string;
  started_at: string;
}

export interface GatewayPresence {
  user_id: string;
  user_name: string;
  user_email: string;
  device_id: string;
  device_name: string;
  platform: string;
  online: boolean;
  last_seen_at: string;
  active_sessions: GatewayActiveSession[];
}

export interface GatewaySMTPSettings {
  enabled: boolean;
  host: string;
  port: number;
  username: string;
  password?: string;
  has_password?: boolean;
  from_name: string;
  from_email: string;
  use_tls: boolean;
  web_base_url: string;
  updated_at?: string;
}

export interface GatewayUserInvite {
  id: string;
  email: string;
  name: string;
  invited_by: string;
  expires_at: string;
  used_at?: string;
  created_at: string;
}

export interface GatewayInviteResult {
  invite: GatewayUserInvite;
  invite_url: string;
  email_sent: boolean;
  email_error?: string;
}

function resolveDefaultApiBase(): string {
  const envBase = (import.meta as any).env?.VITE_API_BASE as string | undefined;
  if (envBase && envBase.trim()) {
    return envBase.trim().replace(/\/+$/, '');
  }
  if (typeof window !== 'undefined' && window.location.origin) {
    return `${window.location.origin}/api/v1`;
  }
  const envPort = ((import.meta as any).env?.VITE_API_PORT as string | undefined)?.trim() || '8080';
  return `http://localhost:${envPort}/api/v1`;
}

class GatewayApiClient {
  private apiBase: string = 'http://localhost:8080/api/v1';
  private token: string | null = null;
  private refreshToken: string | null = null;
  private refreshPromise: Promise<boolean> | null = null;

  constructor() {
    const fallback = resolveDefaultApiBase();
    if (typeof window !== 'undefined') {
      this.token = localStorage.getItem('gateway_admin_token');
      this.refreshToken = localStorage.getItem('gateway_admin_refresh_token');
      const saved = localStorage.getItem('gateway_admin_api_base');
      const pageHost = window.location.hostname;
      const isRemotePage = pageHost && pageHost !== 'localhost' && pageHost !== '127.0.0.1';
      const isStaleLocalhost = saved && /^https?:\/\/(localhost|127\.0\.0\.1)(:|\/|$)/.test(saved);
      const isStaleSameHost8080 =
        saved &&
        window.location.port === '' &&
        saved === `${window.location.protocol}//${pageHost}:8080/api/v1`;
      this.apiBase = saved && !(isRemotePage && isStaleLocalhost) && !isStaleSameHost8080 ? saved : fallback;
    } else {
      this.apiBase = fallback;
    }
  }

  setApiBase(url: string) {
    this.apiBase = url.replace(/\/+$/, '');
    if (typeof window !== 'undefined') {
      localStorage.setItem('gateway_admin_api_base', this.apiBase);
    }
  }

  getApiBase(): string {
    return this.apiBase;
  }

  setToken(token: string, refreshToken?: string) {
    this.token = token;
    if (typeof window !== 'undefined') {
      localStorage.setItem('gateway_admin_token', token);
    }
    if (refreshToken !== undefined) {
      this.refreshToken = refreshToken || null;
      if (typeof window !== 'undefined') {
        if (refreshToken) {
          localStorage.setItem('gateway_admin_refresh_token', refreshToken);
        } else {
          localStorage.removeItem('gateway_admin_refresh_token');
        }
      }
    }
  }

  getToken(): string | null {
    return this.token;
  }

  clearToken() {
    this.token = null;
    this.refreshToken = null;
    if (typeof window !== 'undefined') {
      localStorage.removeItem('gateway_admin_token');
      localStorage.removeItem('gateway_admin_refresh_token');
    }
  }

  private async tryRefresh(): Promise<boolean> {
    if (!this.refreshToken) return false;
    if (this.refreshPromise) return this.refreshPromise;

    this.refreshPromise = (async () => {
      try {
        const res = await this.request<{ user: GatewayUser; token: string; refresh_token?: string }>(
          '/auth/refresh',
          {
            method: 'POST',
            body: JSON.stringify({ refresh_token: this.refreshToken })
          },
          true
        );
        if (!res.token) return false;
        this.setToken(res.token, res.refresh_token ?? this.refreshToken ?? undefined);
        return true;
      } catch {
        return false;
      } finally {
        this.refreshPromise = null;
      }
    })();

    return this.refreshPromise;
  }

  private async request<T>(path: string, options: RequestInit = {}, skipRefresh = false): Promise<T> {
    const headers = new Headers(options.headers || {});
    headers.set('Content-Type', 'application/json');

    if (this.token && path !== '/auth/login' && path !== '/auth/refresh') {
      headers.set('Authorization', `Bearer ${this.token}`);
    }

    const timeoutMs = /\/(smtp|invites)/.test(path) ? 20000 : 10000;
    const controller = !options.signal ? new AbortController() : null;
    const timeoutId = controller ? setTimeout(() => controller.abort(), timeoutMs) : null;

    let res: Response;
    try {
      res = await fetch(`${this.apiBase}${path}`, {
        ...options,
        headers,
        signal: options.signal ?? controller?.signal
      });
    } catch (err: any) {
      if (err?.name === 'AbortError' || controller?.signal.aborted) {
        throw new Error(`Request timeout (${timeoutMs / 1000}s) — cek koneksi ke Gateway API (${this.apiBase})`);
      }
      throw err;
    } finally {
      if (timeoutId) clearTimeout(timeoutId);
    }

    if (
      res.status === 401 &&
      !skipRefresh &&
      path !== '/auth/login' &&
      path !== '/auth/refresh' &&
      this.refreshToken
    ) {
      const ok = await this.tryRefresh();
      if (ok) return this.request<T>(path, options, true);
    }

    if (!res.ok) {
      let msg = `Gateway Error: ${res.status} ${res.statusText}`;
      try {
        const body = await res.json();
        if (body.error) msg = body.error;
      } catch {}
      const err = new Error(msg) as Error & { status?: number };
      err.status = res.status;
      throw err;
    }

    return res.json() as Promise<T>;
  }

  async login(email: string, password: string) {
    const res = await this.request<{ user: GatewayUser; token: string; refresh_token?: string }>('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ email, password })
    });
    this.setToken(res.token, res.refresh_token);
    return res;
  }

  async register(email: string, password: string, name: string) {
    const res = await this.request<{ user: GatewayUser; token: string; refresh_token?: string }>('/auth/register', {
      method: 'POST',
      body: JSON.stringify({ email, password, name })
    });
    this.setToken(res.token, res.refresh_token);
    return res;
  }

  async getMe(): Promise<GatewayUser> {
    return this.request<GatewayUser>('/auth/me');
  }

  async updateMe(payload: {
    current_password: string;
    name?: string;
    email?: string;
    new_password?: string;
  }): Promise<GatewayUser> {
    return this.request<GatewayUser>('/auth/me', {
      method: 'PUT',
      body: JSON.stringify(payload)
    });
  }

  async getUsers(): Promise<GatewayUser[]> {
    return this.request<GatewayUser[]>('/users');
  }

  async createUser(email: string, password: string, name: string): Promise<GatewayUser> {
    return this.request<GatewayUser>('/users', {
      method: 'POST',
      body: JSON.stringify({ email, password, name })
    });
  }

  async deleteUser(userId: string): Promise<void> {
    await this.request<{ status: string }>(`/users/${userId}`, {
      method: 'DELETE'
    });
  }

  async getVaults(): Promise<GatewayVault[]> {
    return this.request<GatewayVault[]>('/vaults');
  }

  async createVault(name: string, type: 'personal' | 'team' = 'team'): Promise<GatewayVault> {
    return this.request<GatewayVault>('/vaults', {
      method: 'POST',
      body: JSON.stringify({
        name,
        type,
        encrypted_key: `e2ee-wrapped-${type}-key-${Date.now()}`
      })
    });
  }

  async getVaultMembers(vaultId: string): Promise<GatewayMember[]> {
    return this.request<GatewayMember[]>(`/vaults/${vaultId}/members`);
  }

  async inviteMember(vaultId: string, email: string, role: string = 'member'): Promise<GatewayMember> {
    return this.request<GatewayMember>(`/vaults/${vaultId}/members`, {
      method: 'POST',
      body: JSON.stringify({ email, role })
    });
  }

  async getSessionLogs(): Promise<GatewaySessionLog[]> {
    return this.request<GatewaySessionLog[]>('/logs');
  }

  async getPresence(): Promise<GatewayPresence[]> {
    return this.request<GatewayPresence[]>('/presence');
  }

  async getSMTPSettings(): Promise<GatewaySMTPSettings> {
    return this.request<GatewaySMTPSettings>('/settings/smtp');
  }

  async updateSMTPSettings(cfg: GatewaySMTPSettings): Promise<GatewaySMTPSettings> {
    return this.request<GatewaySMTPSettings>('/settings/smtp', {
      method: 'PUT',
      body: JSON.stringify(cfg)
    });
  }

  async sendTestEmail(toEmail: string): Promise<void> {
    await this.request<{ status: string }>('/settings/smtp/test', {
      method: 'POST',
      body: JSON.stringify({ to_email: toEmail })
    });
  }

  async listInvites(): Promise<GatewayUserInvite[]> {
    return this.request<GatewayUserInvite[]>('/invites');
  }

  async createInvite(name: string, email: string): Promise<GatewayInviteResult> {
    return this.request<GatewayInviteResult>('/invites', {
      method: 'POST',
      body: JSON.stringify({ name, email })
    });
  }

  async deleteInvite(id: string): Promise<void> {
    await this.request<{ status: string }>(`/invites/${id}`, {
      method: 'DELETE'
    });
  }

  async resendInvite(id: string): Promise<GatewayInviteResult> {
    return this.request<GatewayInviteResult>(`/invites/${id}/resend`, {
      method: 'POST'
    });
  }

  async verifyInvite(token: string): Promise<{ email: string; name: string; expires_at: string }> {
    return this.request<{ email: string; name: string; expires_at: string }>(
      `/invites/verify?token=${encodeURIComponent(token)}`,
      {},
      true
    );
  }

  async acceptInvite(token: string, password: string): Promise<{ user: GatewayUser; token: string }> {
    return this.request<{ user: GatewayUser; token: string }>(
      '/invites/accept',
      {
        method: 'POST',
        body: JSON.stringify({ token, password })
      },
      true
    );
  }
}

export const gatewayApi = new GatewayApiClient();
