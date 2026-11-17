// OpenTermius Gateway Admin API Client

export interface GatewayUser {
  id: string;
  email: string;
  name: string;
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

function resolveDefaultApiBase(): string {
  const envBase = (import.meta as any).env?.VITE_API_BASE as string | undefined;
  if (envBase && envBase.trim()) {
    return envBase.trim().replace(/\/+$/, '');
  }
  const envPort = ((import.meta as any).env?.VITE_API_PORT as string | undefined)?.trim() || '8080';
  if (typeof window !== 'undefined') {
    const { protocol, hostname } = window.location;
    if (hostname) {
      return `${protocol}//${hostname}:${envPort}/api/v1`;
    }
  }
  return `http://localhost:${envPort}/api/v1`;
}

class GatewayApiClient {
  private apiBase: string = 'http://localhost:8080/api/v1';
  private token: string | null = null;

  constructor() {
    const fallback = resolveDefaultApiBase();
    if (typeof window !== 'undefined') {
      this.token = localStorage.getItem('gateway_admin_token');
      const saved = localStorage.getItem('gateway_admin_api_base');
      const pageHost = window.location.hostname;
      const isRemotePage = pageHost && pageHost !== 'localhost' && pageHost !== '127.0.0.1';
      const isStaleLocalhost = saved && /^https?:\/\/(localhost|127\.0\.0\.1)(:|\/|$)/.test(saved);
      this.apiBase = saved && !(isRemotePage && isStaleLocalhost) ? saved : fallback;
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

  setToken(token: string) {
    this.token = token;
    if (typeof window !== 'undefined') {
      localStorage.setItem('gateway_admin_token', token);
    }
  }

  getToken(): string | null {
    return this.token;
  }

  clearToken() {
    this.token = null;
    if (typeof window !== 'undefined') {
      localStorage.removeItem('gateway_admin_token');
    }
  }

  private async request<T>(path: string, options: RequestInit = {}): Promise<T> {
    const headers = new Headers(options.headers || {});
    headers.set('Content-Type', 'application/json');

    if (this.token) {
      headers.set('Authorization', `Bearer ${this.token}`);
    }

    const res = await fetch(`${this.apiBase}${path}`, {
      ...options,
      headers
    });

    if (!res.ok) {
      let msg = `Gateway Error: ${res.status} ${res.statusText}`;
      try {
        const body = await res.json();
        if (body.error) msg = body.error;
      } catch {}
      throw new Error(msg);
    }

    return res.json() as Promise<T>;
  }

  async login(email: string, password: string) {
    const res = await this.request<{ user: GatewayUser; token: string }>('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ email, password })
    });
    this.setToken(res.token);
    return res;
  }

  async register(email: string, password: string, name: string) {
    const res = await this.request<{ user: GatewayUser; token: string }>('/auth/register', {
      method: 'POST',
      body: JSON.stringify({ email, password, name })
    });
    this.setToken(res.token);
    return res;
  }

  async getMe(): Promise<GatewayUser> {
    return this.request<GatewayUser>('/auth/me');
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
}

export const gatewayApi = new GatewayApiClient();
