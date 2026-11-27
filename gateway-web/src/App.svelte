<script lang="ts">
  import { onMount } from 'svelte';
  import AcceptInviteView from './lib/components/AcceptInviteView.svelte';
  import {
    gatewayApi,
    type GatewayUser,
    type GatewayVault,
    type GatewayMember,
    type GatewayPresence,
    type GatewaySMTPSettings,
    type GatewayUserInvite
  } from './lib/api/gateway_client';
  import {
    Server,
    Users,
    Activity,
    Settings,
    LogOut,
    RefreshCw,
    CheckCircle2,
    AlertCircle,
    UserPlus,
    Trash2,
    Lock,
    FolderPlus,
    Menu,
    X,
    Search,
    Mail,
    Send,
    Copy,
    Check,
    Eye,
    EyeOff
  } from '@lucide/svelte';

  type TabId = 'overview' | 'users' | 'teams' | 'settings';

  // Navigation & Auth State
  let activeTab = $state<TabId>('overview');
  let isAuthenticated = $state(Boolean(gatewayApi.getToken()));
  let currentUser = $state<GatewayUser | null>(null);
  let isMobileOpen = $state(false);

  // Auth Form State (no hardcoded credentials exposed in UI)
  let authEmail = $state('');
  let authPassword = $state('');
  let showAuthPassword = $state(false);
  let authError = $state<string | null>(null);
  let isAuthSubmitting = $state(false);
  let confirmDialog = $state<{
    title: string;
    message: string;
    confirmLabel: string;
    onConfirm: () => Promise<void>;
  } | null>(null);

  // Gateway Data State
  let backendUrl = $state(gatewayApi.getApiBase());
  let users = $state<GatewayUser[]>([]);
  let userSearch = $state('');
  let vaults = $state<GatewayVault[]>([]);
  let selectedTeamVaultId = $state('');
  let teamMembers = $state<GatewayMember[]>([]);
  let presenceList = $state<GatewayPresence[]>([]);
  let loadState = $state<'loading' | 'success' | 'error'>('loading');
  let isRefreshing = $state(false);
  let errorMessage = $state<string | null>(null);
  let statusBanner = $state<string | null>(null);

  // Modals
  let isCreateUserOpen = $state(false);
  let newUserName = $state('');
  let newUserEmail = $state('');
  let newUserPassword = $state('');
  let isCreatingUser = $state(false);
  let createUserError = $state<string | null>(null);

  let isCreateVaultOpen = $state(false);
  let newVaultName = $state('');
  let isCreatingVault = $state(false);
  let createVaultError = $state<string | null>(null);

  let isInviteOpen = $state(false);
  let inviteEmail = $state('');
  let inviteRole = $state<'admin' | 'member'>('member');
  let isInviting = $state(false);
  let inviteError = $state<string | null>(null);

  // Email User Invite (Admin sends invite link so user creates their own password)
  let pendingInvites = $state<GatewayUserInvite[]>([]);
  let resendingInviteId = $state<string | null>(null);
  let isEmailInviteOpen = $state(false);
  let emailInviteName = $state('');
  let emailInviteAddr = $state('');
  let isSendingInvite = $state(false);
  let emailInviteError = $state<string | null>(null);
  let lastInviteLink = $state<string | null>(null);
  let copiedInviteLink = $state(false);

  // SMTP Settings (stored in PostgreSQL)
  let smtpConfig = $state<GatewaySMTPSettings>({
    enabled: false,
    host: '',
    port: 587,
    username: '',
    password: '',
    has_password: false,
    from_name: 'OpenTermius Gateway',
    from_email: '',
    use_tls: true,
    web_base_url: typeof window !== 'undefined' ? window.location.origin : ''
  });
  let isSavingSmtp = $state(false);
  let smtpError = $state<string | null>(null);
  let testEmailTo = $state('');
  let isTestingSmtp = $state(false);

  // Admin Account Profile (Change Name / Email / Password)
  let profileName = $state('');
  let profileEmail = $state('');
  let profileCurrentPassword = $state('');
  let profileNewPassword = $state('');
  let profileConfirmPassword = $state('');
  let isSavingProfile = $state(false);
  let profileError = $state<string | null>(null);

  // Public Invite Acceptance (?invite=<token>)
  let inviteToken = $state<string | null>(
    typeof window !== 'undefined' ? new URLSearchParams(window.location.search).get('invite') : null
  );
  let inviteVerifyState = $state<'loading' | 'ready' | 'done' | 'invalid'>('loading');
  let inviteInfo = $state<{ email: string; name: string; expires_at: string } | null>(null);
  let inviteVerifyError = $state<string | null>(null);
  let isAcceptingInvite = $state(false);
  let acceptError = $state<string | null>(null);

  let teamVaults = $derived(vaults.filter((v) => v.type === 'team'));
  let onlineDevices = $derived(presenceList.filter((p) => p.online));
  let activeSshCount = $derived(
    onlineDevices.reduce((acc, p) => acc + p.active_sessions.length, 0)
  );
  let filteredUsers = $derived(
    users.filter((u) => {
      const q = userSearch.trim().toLowerCase();
      if (!q) return true;
      return u.name.toLowerCase().includes(q) || u.email.toLowerCase().includes(q);
    })
  );
  let apiHostLabel = $derived.by(() => {
    try {
      return new URL(backendUrl).host;
    } catch {
      return backendUrl;
    }
  });

  const TAB_TITLES: Record<TabId, string> = {
    overview: 'Overview',
    users: 'Users',
    teams: 'Teams & Vaults',
    settings: 'Settings'
  };

  function showBanner(msg: string) {
    statusBanner = msg;
    setTimeout(() => {
      if (statusBanner === msg) statusBanner = null;
    }, 3500);
  }

  function handleWindowKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      confirmDialog = null;
      isCreateUserOpen = false;
      isCreateVaultOpen = false;
      isInviteOpen = false;
      isEmailInviteOpen = false;
      isMobileOpen = false;
    }
  }

  async function checkAuthAndLoad() {
    loadState = 'loading';
    errorMessage = null;

    if (!gatewayApi.getToken()) {
      isAuthenticated = false;
      loadState = 'success';
      return;
    }

    try {
      currentUser = await gatewayApi.getMe();
      profileName = currentUser.name;
      profileEmail = currentUser.email;
      isAuthenticated = true;
      await refreshData();
    } catch (err: unknown) {
      const status = (err as { status?: number } | null)?.status;
      if (status === 401) {
        gatewayApi.clearToken();
        isAuthenticated = false;
        loadState = 'success';
      } else {
        loadState = 'error';
        errorMessage = err instanceof Error ? err.message : 'Cannot reach Gateway API';
      }
    }
  }

  async function refreshData() {
    isRefreshing = true;
    errorMessage = null;

    try {
      const [loadedUsers, loadedVaults, loadedPresence, loadedInvites, loadedSmtp] = await Promise.all([
        gatewayApi.getUsers().catch(() => [] as GatewayUser[]),
        gatewayApi.getVaults(),
        gatewayApi.getPresence().catch(() => [] as GatewayPresence[]),
        gatewayApi.listInvites().catch(() => [] as GatewayUserInvite[]),
        gatewayApi.getSMTPSettings().catch(() => null)
      ]);

      users = loadedUsers;
      vaults = loadedVaults;
      presenceList = loadedPresence;
      pendingInvites = loadedInvites;
      if (loadedSmtp) {
        smtpConfig = {
          ...loadedSmtp,
          password: '',
          web_base_url: loadedSmtp.web_base_url || (typeof window !== 'undefined' ? window.location.origin : '')
        };
      }

      const tVaults = loadedVaults.filter((v) => v.type === 'team');
      if (tVaults.length > 0) {
        if (!selectedTeamVaultId || !tVaults.some((v) => v.id === selectedTeamVaultId)) {
          selectedTeamVaultId = tVaults[0].id;
        }
        await loadVaultMembers(selectedTeamVaultId);
      } else {
        selectedTeamVaultId = '';
        teamMembers = [];
      }

      loadState = 'success';
    } catch (err: unknown) {
      loadState = 'error';
      errorMessage = err instanceof Error ? err.message : 'Cannot reach Gateway API';
    } finally {
      isRefreshing = false;
    }
  }

  async function loadVaultMembers(vaultId: string) {
    if (!vaultId) {
      teamMembers = [];
      return;
    }
    try {
      teamMembers = await gatewayApi.getVaultMembers(vaultId);
    } catch {
      teamMembers = [];
    }
  }

  async function handleAuthSubmit(e?: SubmitEvent) {
    e?.preventDefault();
    if (!authEmail.trim() || !authPassword) {
      authError = 'Email and password are required.';
      return;
    }

    isAuthSubmitting = true;
    authError = null;

    try {
      gatewayApi.setApiBase(backendUrl.trim());
      const res = await gatewayApi.login(authEmail.trim(), authPassword);
      currentUser = res.user;
      profileName = res.user.name;
      profileEmail = res.user.email;
      isAuthenticated = true;
      authPassword = '';
      await refreshData();
    } catch (err: unknown) {
      authError = err instanceof Error ? err.message : 'Sign in failed';
    } finally {
      isAuthSubmitting = false;
    }
  }

  async function handleCreateUser(e?: SubmitEvent) {
    e?.preventDefault();
    if (!newUserName.trim() || !newUserEmail.trim() || !newUserPassword) {
      createUserError = 'Full name, email, and password are required.';
      return;
    }

    isCreatingUser = true;
    createUserError = null;

    try {
      const created = await gatewayApi.createUser(
        newUserEmail.trim(),
        newUserPassword,
        newUserName.trim()
      );
      users = [created, ...users];
      isCreateUserOpen = false;
      newUserName = '';
      newUserEmail = '';
      newUserPassword = '';
      showBanner(`Created user ${created.email}.`);
    } catch (err: unknown) {
      createUserError = err instanceof Error ? err.message : 'Failed to create user';
    } finally {
      isCreatingUser = false;
    }
  }

  function handleDeleteUser(u: GatewayUser) {
    if (u.id === currentUser?.id) return;
    confirmDialog = {
      title: 'Delete User',
      message: `Delete user "${u.name}" (${u.email})? This cannot be undone.`,
      confirmLabel: 'Delete',
      onConfirm: async () => {
        try {
          await gatewayApi.deleteUser(u.id);
          users = users.filter((item) => item.id !== u.id);
          showBanner(`Deleted ${u.email}.`);
        } catch (err: unknown) {
          errorMessage = err instanceof Error ? err.message : 'Failed to delete user';
        }
      }
    };
  }

  function handleRemoveVaultMember(member: GatewayMember) {
    if (!selectedTeamVaultId || member.role === 'owner') return;
    const display = member.name || member.email || member.user_id;
    confirmDialog = {
      title: 'Remove Vault Member',
      message: `Remove "${display}" from this team vault?`,
      confirmLabel: 'Remove',
      onConfirm: async () => {
        try {
          await gatewayApi.removeMember(selectedTeamVaultId, member.user_id);
          teamMembers = teamMembers.filter((m) => m.id !== member.id);
          showBanner(`Removed ${display} from team vault.`);
        } catch (err: unknown) {
          errorMessage = err instanceof Error ? err.message : 'Failed to remove member';
        }
      }
    };
  }

  async function handleCreateTeamVault(e?: SubmitEvent) {
    e?.preventDefault();
    if (!newVaultName.trim()) {
      createVaultError = 'Team vault name is required.';
      return;
    }

    isCreatingVault = true;
    createVaultError = null;

    try {
      const created = await gatewayApi.createVault(newVaultName.trim(), 'team');
      vaults = [created, ...vaults];
      selectedTeamVaultId = created.id;
      await loadVaultMembers(created.id);
      isCreateVaultOpen = false;
      newVaultName = '';
      showBanner(`Created vault "${created.name}".`);
    } catch (err: unknown) {
      createVaultError = err instanceof Error ? err.message : 'Failed to create vault';
    } finally {
      isCreatingVault = false;
    }
  }

  async function handleInviteMember(e?: SubmitEvent) {
    e?.preventDefault();
    if (!inviteEmail.trim() || !selectedTeamVaultId) {
      inviteError = 'User email and target vault are required.';
      return;
    }

    isInviting = true;
    inviteError = null;

    try {
      const added = await gatewayApi.inviteMember(
        selectedTeamVaultId,
        inviteEmail.trim(),
        inviteRole
      );
      teamMembers = [added, ...teamMembers];
      isInviteOpen = false;
      inviteEmail = '';
      showBanner(`Added ${added.email || inviteEmail} to team vault.`);
    } catch (err: unknown) {
      inviteError = err instanceof Error ? err.message : 'Failed to add member';
    } finally {
      isInviting = false;
    }
  }

  async function handleSendEmailInvite(e?: SubmitEvent) {
    e?.preventDefault();
    if (!emailInviteName.trim() || !emailInviteAddr.trim()) {
      emailInviteError = 'Name and email are required.';
      return;
    }
    isSendingInvite = true;
    emailInviteError = null;
    lastInviteLink = null;
    try {
      const res = await gatewayApi.createInvite(emailInviteName.trim(), emailInviteAddr.trim());
      pendingInvites = [res.invite, ...pendingInvites];
      lastInviteLink = res.invite_url;
      emailInviteName = '';
      emailInviteAddr = '';
      if (res.email_sent) {
        showBanner(`Invite email sent to ${res.invite.email}.`);
      } else if (res.email_error) {
        emailInviteError = `Invite created, but email failed: ${res.email_error}. Copy the invite link below instead.`;
      } else {
        showBanner(`Invite created for ${res.invite.email}. Enable SMTP in Settings or copy the link.`);
      }
    } catch (err: unknown) {
      emailInviteError = err instanceof Error ? err.message : 'Failed to create invite';
    } finally {
      isSendingInvite = false;
    }
  }

  function handleRevokeInvite(inv: GatewayUserInvite) {
    confirmDialog = {
      title: 'Revoke Invite',
      message: `Revoke invite for ${inv.email}?`,
      confirmLabel: 'Revoke',
      onConfirm: async () => {
        try {
          await gatewayApi.deleteInvite(inv.id);
          pendingInvites = pendingInvites.filter((i) => i.id !== inv.id);
          showBanner(`Revoked invite for ${inv.email}.`);
        } catch (err: unknown) {
          errorMessage = err instanceof Error ? err.message : 'Failed to revoke invite';
        }
      }
    };
  }

  async function handleResendInvite(inv: GatewayUserInvite) {
    resendingInviteId = inv.id;
    errorMessage = null;
    try {
      const res = await gatewayApi.resendInvite(inv.id);
      pendingInvites = [res.invite, ...pendingInvites.filter((i) => i.id !== inv.id)];
      if (res.email_sent) {
        showBanner(`Resent invite email to ${inv.email}.`);
      } else if (res.email_error) {
        errorMessage = `New invite link generated, but email failed: ${res.email_error}`;
      } else {
        showBanner(`Refreshed invite for ${inv.email}. Enable SMTP in Settings to send emails.`);
      }
    } catch (err: unknown) {
      errorMessage = err instanceof Error ? err.message : 'Failed to resend invite';
    } finally {
      resendingInviteId = null;
    }
  }

  async function handleSaveProfile(e?: SubmitEvent) {
    e?.preventDefault();
    if (!profileCurrentPassword) {
      profileError = 'Current password is required to save changes.';
      return;
    }
    if (profileNewPassword && profileNewPassword.length < 8) {
      profileError = 'New password must be at least 8 characters.';
      return;
    }
    if (profileNewPassword && profileNewPassword !== profileConfirmPassword) {
      profileError = 'New passwords do not match.';
      return;
    }

    isSavingProfile = true;
    profileError = null;
    try {
      const updated = await gatewayApi.updateMe({
        current_password: profileCurrentPassword,
        name: profileName.trim(),
        email: profileEmail.trim(),
        new_password: profileNewPassword || undefined
      });
      currentUser = updated;
      profileName = updated.name;
      profileEmail = updated.email;
      profileCurrentPassword = '';
      profileNewPassword = '';
      profileConfirmPassword = '';
      users = users.map((u) => (u.id === updated.id ? updated : u));
      showBanner('Admin account updated successfully.');
    } catch (err: unknown) {
      profileError = err instanceof Error ? err.message : 'Failed to update account';
    } finally {
      isSavingProfile = false;
    }
  }

  async function handleSaveSmtp(e?: SubmitEvent) {
    e?.preventDefault();
    isSavingSmtp = true;
    smtpError = null;
    try {
      const saved = await gatewayApi.updateSMTPSettings({
        ...smtpConfig,
        port: Number(smtpConfig.port) || 587
      });
      smtpConfig = { ...saved, password: '' };
      showBanner('SMTP settings saved to database.');
    } catch (err: unknown) {
      smtpError = err instanceof Error ? err.message : 'Failed to save SMTP settings';
    } finally {
      isSavingSmtp = false;
    }
  }

  async function handleTestSmtp(e?: SubmitEvent) {
    e?.preventDefault();
    const target = testEmailTo.trim() || currentUser?.email || '';
    if (!target) {
      smtpError = 'Enter a recipient email to send a test.';
      return;
    }
    isTestingSmtp = true;
    smtpError = null;
    try {
      await gatewayApi.sendTestEmail(target);
      showBanner(`Test email sent to ${target}.`);
    } catch (err: unknown) {
      smtpError = err instanceof Error ? err.message : 'SMTP test failed';
    } finally {
      isTestingSmtp = false;
    }
  }

  async function handleAcceptInvite(password: string, confirmPassword: string) {
    if (!inviteToken) return;
    if (password.length < 8) {
      acceptError = 'Password must be at least 8 characters.';
      return;
    }
    if (password !== confirmPassword) {
      acceptError = 'Passwords do not match.';
      return;
    }
    isAcceptingInvite = true;
    acceptError = null;
    try {
      await gatewayApi.acceptInvite(inviteToken, password);
      inviteVerifyState = 'done';
    } catch (err: unknown) {
      acceptError = err instanceof Error ? err.message : 'Failed to activate account';
    } finally {
      isAcceptingInvite = false;
    }
  }

  async function copyInviteUrl(url: string) {
    try {
      await navigator.clipboard.writeText(url);
      copiedInviteLink = true;
      setTimeout(() => (copiedInviteLink = false), 2000);
    } catch {
      // ignore
    }
  }

  function handleLogout() {
    gatewayApi.clearToken();
    isAuthenticated = false;
    currentUser = null;
    users = [];
    vaults = [];
    teamMembers = [];
    presenceList = [];
    pendingInvites = [];
  }

  onMount(() => {
    if (inviteToken) {
      inviteVerifyState = 'loading';
      gatewayApi
        .verifyInvite(inviteToken)
        .then((info) => {
          inviteInfo = info;
          inviteVerifyState = 'ready';
        })
        .catch((err: unknown) => {
          inviteVerifyError = err instanceof Error ? err.message : 'Invite link is invalid or expired.';
          inviteVerifyState = 'invalid';
        });
      return;
    }

    checkAuthAndLoad();
    const timer = setInterval(() => {
      if (isAuthenticated && activeTab === 'overview' && !document.hidden) {
        gatewayApi.getPresence().then((list) => (presenceList = list)).catch(() => {});
      }
    }, 8000);
    return () => clearInterval(timer);
  });
</script>

<svelte:window onkeydown={handleWindowKeydown} />

{#if inviteToken}
  <AcceptInviteView
    verifyState={inviteVerifyState}
    {inviteInfo}
    verifyError={inviteVerifyError}
    {acceptError}
    isAccepting={isAcceptingInvite}
    onAccept={handleAcceptInvite}
  />
{:else}
<div class="flex h-screen w-screen overflow-hidden bg-[#0a0a0c] text-neutral-100 font-sans">
  {#if isMobileOpen}
    <div
      class="fixed inset-0 z-40 bg-black/70 backdrop-blur-xs md:hidden"
      onclick={() => (isMobileOpen = false)}
      role="presentation"
    ></div>
  {/if}

  {#if isAuthenticated}
    <!-- Sidebar -->
    <aside
      class="flex flex-col justify-between border-r border-white/[0.06] bg-[#0e0e13] p-3.5 shrink-0 transition-transform duration-200
        fixed inset-y-0 left-0 z-50 w-60 shadow-2xl md:relative md:z-auto md:shadow-none
        {isMobileOpen ? 'translate-x-0' : '-translate-x-full md:translate-x-0'}"
    >
      <div class="space-y-5">
        <!-- Brand -->
        <div class="flex items-center justify-between px-1.5 pt-1">
          <div class="flex items-center gap-2.5">
            <div class="flex h-8 w-8 items-center justify-center rounded-lg bg-emerald-500/10 border border-emerald-500/20 text-emerald-400">
              <Server class="h-4 w-4" />
            </div>
            <div>
              <div class="flex items-center gap-1.5">
                <span class="text-sm font-semibold text-white tracking-tight">OpenTermius</span>
                <span class="rounded bg-white/[0.06] px-1.5 py-0.5 text-[9px] font-mono text-neutral-300">GATEWAY</span>
              </div>
              <p class="text-[10px] text-neutral-500">Admin Console</p>
            </div>
          </div>
          {#if isMobileOpen}
            <button
              type="button"
              onclick={() => (isMobileOpen = false)}
              class="md:hidden text-neutral-400 hover:text-white p-1 rounded-lg hover:bg-white/[0.06] transition"
              aria-label="Close sidebar"
            >
              <X class="h-4 w-4" />
            </button>
          {/if}
        </div>

        <!-- Navigation -->
        <nav class="space-y-1 text-xs">
          <button
            type="button"
            onclick={() => { activeTab = 'overview'; isMobileOpen = false; }}
            class="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 transition cursor-pointer {activeTab === 'overview' ? 'bg-emerald-500/12 text-emerald-400 font-medium' : 'text-neutral-400 hover:bg-white/[0.04] hover:text-white'}"
          >
            <Activity class="h-4 w-4" />
            <span>Overview</span>
          </button>

          <button
            type="button"
            onclick={() => { activeTab = 'users'; isMobileOpen = false; }}
            class="flex w-full items-center justify-between rounded-lg px-2.5 py-2 transition cursor-pointer {activeTab === 'users' ? 'bg-emerald-500/12 text-emerald-400 font-medium' : 'text-neutral-400 hover:bg-white/[0.04] hover:text-white'}"
          >
            <span class="flex items-center gap-2.5">
              <UserPlus class="h-4 w-4" />
              <span>Users</span>
            </span>
            <span class="rounded bg-white/[0.05] px-1.5 py-0.5 text-[10px] font-mono text-neutral-400">{users.length}</span>
          </button>

          <button
            type="button"
            onclick={() => { activeTab = 'teams'; isMobileOpen = false; }}
            class="flex w-full items-center justify-between rounded-lg px-2.5 py-2 transition cursor-pointer {activeTab === 'teams' ? 'bg-emerald-500/12 text-emerald-400 font-medium' : 'text-neutral-400 hover:bg-white/[0.04] hover:text-white'}"
          >
            <span class="flex items-center gap-2.5">
              <Users class="h-4 w-4" />
              <span>Teams &amp; Vaults</span>
            </span>
            <span class="rounded bg-white/[0.05] px-1.5 py-0.5 text-[10px] font-mono text-neutral-400">{teamVaults.length}</span>
          </button>

          <button
            type="button"
            onclick={() => { activeTab = 'settings'; isMobileOpen = false; }}
            class="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 transition cursor-pointer {activeTab === 'settings' ? 'bg-emerald-500/12 text-emerald-400 font-medium' : 'text-neutral-400 hover:bg-white/[0.04] hover:text-white'}"
          >
            <Settings class="h-4 w-4" />
            <span>Settings</span>
          </button>
        </nav>
      </div>

      <!-- Bottom Status + User -->
      <div class="space-y-3">
        <div class="rounded-lg border border-white/[0.06] bg-[#13131a] px-2.5 py-2 text-[11px]">
          <div class="flex items-center gap-2">
            <span class="h-1.5 w-1.5 rounded-full {loadState === 'error' ? 'bg-rose-400' : 'bg-emerald-400'}"></span>
            <span class="font-medium {loadState === 'error' ? 'text-rose-300' : 'text-neutral-200'}">
              {loadState === 'error' ? 'Gateway Unreachable' : 'Gateway Online'}
            </span>
          </div>
          <p class="mt-0.5 truncate font-mono text-[10px] text-neutral-500" title={backendUrl}>{apiHostLabel}</p>
        </div>

        {#if currentUser}
          <div class="border-t border-white/[0.06] pt-3 flex items-center justify-between text-xs">
            <div class="min-w-0 pr-2">
              <p class="font-medium text-neutral-100 truncate">{currentUser.name}</p>
              <p class="text-[10px] text-neutral-500 truncate">{currentUser.email}</p>
            </div>
            <button
              type="button"
              onclick={handleLogout}
              class="text-neutral-500 hover:text-rose-400 p-1.5 rounded-lg hover:bg-white/[0.05] transition cursor-pointer"
              title="Sign Out"
              aria-label="Sign Out"
            >
              <LogOut class="h-4 w-4" />
            </button>
          </div>
        {/if}
      </div>
    </aside>
  {/if}

  <!-- Main Viewport -->
  <main class="flex flex-1 flex-col min-w-0 overflow-hidden">
    {#if isAuthenticated}
      <header class="flex h-12 items-center justify-between border-b border-white/[0.06] bg-[#0d0d12] px-4 sm:px-6 shrink-0">
        <div class="flex items-center gap-2.5">
          <button
            type="button"
            onclick={() => (isMobileOpen = !isMobileOpen)}
            class="md:hidden text-neutral-400 hover:text-white p-1.5 rounded-lg hover:bg-white/[0.06] transition"
            aria-label="Toggle menu"
          >
            <Menu class="h-4 w-4 text-emerald-400" />
          </button>
          <h1 class="text-xs font-semibold text-neutral-200">{TAB_TITLES[activeTab]}</h1>
        </div>

        <button
          type="button"
          disabled={isRefreshing}
          onclick={refreshData}
          class="flex items-center gap-1.5 rounded-lg border border-white/[0.08] bg-[#14141c] px-2.5 py-1.5 text-xs text-neutral-300 hover:text-white hover:border-white/[0.14] disabled:opacity-50 transition cursor-pointer"
        >
          <RefreshCw class="h-3 w-3 {isRefreshing ? 'animate-spin' : ''}" />
          <span>Refresh</span>
        </button>
      </header>
    {/if}

    <div class="flex-1 overflow-y-auto p-4 sm:p-6">
      {#if statusBanner}
        <div class="mb-4 max-w-5xl rounded-xl border border-emerald-500/30 bg-emerald-950/30 px-3.5 py-2.5 text-xs text-emerald-300 flex items-center justify-between">
          <span class="flex items-center gap-2">
            <CheckCircle2 class="h-4 w-4 text-emerald-400 shrink-0" />
            {statusBanner}
          </span>
          <button type="button" onclick={() => (statusBanner = null)} class="text-neutral-400 hover:text-white">
            <X class="h-3.5 w-3.5" />
          </button>
        </div>
      {/if}

      {#if errorMessage && isAuthenticated}
        <div class="mb-4 max-w-5xl rounded-xl border border-rose-500/30 bg-rose-950/30 px-3.5 py-2.5 text-xs text-rose-300 flex items-center justify-between gap-3">
          <span class="flex items-center gap-2">
            <AlertCircle class="h-4 w-4 text-rose-400 shrink-0" />
            {errorMessage}
          </span>
          <button
            type="button"
            onclick={refreshData}
            class="rounded-md bg-rose-500/20 px-2.5 py-1 text-[11px] font-medium text-rose-200 hover:bg-rose-500/30 shrink-0"
          >
            Retry
          </button>
        </div>
      {/if}

      {#if !isAuthenticated}
        <!-- Clean Gateway Admin Login -->
        <div class="flex min-h-full items-center justify-center">
          <form
            onsubmit={handleAuthSubmit}
            class="w-full max-w-sm rounded-2xl border border-white/[0.08] bg-[#121218] p-6 shadow-2xl space-y-4 text-xs"
          >
            <div class="text-center space-y-1">
              <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 mx-auto mb-2.5">
                <Lock class="h-5 w-5" />
              </div>
              <h2 class="text-base font-semibold text-white">OpenTermius Gateway</h2>
              <p class="text-neutral-400">Sign in with your Gateway Admin account</p>
            </div>

            {#if authError}
              <div class="rounded-lg bg-rose-950/40 border border-rose-500/30 p-2.5 text-rose-300 text-[11px]">
                {authError}
              </div>
            {/if}

            <div class="space-y-3">
              <div>
                <label for="adm-api" class="block font-medium text-neutral-400 mb-1">Gateway API</label>
                <input
                  id="adm-api"
                  type="url"
                  bind:value={backendUrl}
                  placeholder="https://gateway.example.com/api/v1"
                  class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 font-mono text-[11px] text-neutral-200 outline-none focus:border-emerald-500/70"
                />
              </div>
              <div>
                <label for="adm-email" class="block font-medium text-neutral-400 mb-1">Admin Email</label>
                <input
                  id="adm-email"
                  type="email"
                  required
                  autocomplete="username"
                  bind:value={authEmail}
                  placeholder="admin@opentermius.local"
                  class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white outline-none focus:border-emerald-500/70"
                />
              </div>
              <div>
                <label for="adm-pw" class="block font-medium text-neutral-400 mb-1">Password</label>
                <div class="relative">
                  <input
                    id="adm-pw"
                    type={showAuthPassword ? 'text' : 'password'}
                    required
                    autocomplete="current-password"
                    bind:value={authPassword}
                    placeholder="Enter password"
                    class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] pl-3 pr-9 py-2 text-white outline-none focus:border-emerald-500/70"
                  />
                  <button
                    type="button"
                    onclick={() => (showAuthPassword = !showAuthPassword)}
                    class="absolute right-2 top-1/2 -translate-y-1/2 rounded p-1 text-neutral-400 hover:text-white transition cursor-pointer"
                    aria-label={showAuthPassword ? 'Hide password' : 'Show password'}
                  >
                    {#if showAuthPassword}
                      <EyeOff class="h-3.5 w-3.5" />
                    {:else}
                      <Eye class="h-3.5 w-3.5" />
                    {/if}
                  </button>
                </div>
              </div>

              <button
                type="submit"
                disabled={isAuthSubmitting}
                class="w-full rounded-lg bg-emerald-600 py-2.5 font-semibold text-white hover:bg-emerald-500 transition disabled:opacity-50 cursor-pointer"
              >
                {isAuthSubmitting ? 'Signing in...' : 'Sign In'}
              </button>
            </div>
          </form>
        </div>
      {:else if activeTab === 'overview'}
        <div class="space-y-5 max-w-5xl">
          <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
            <div>
              <h2 class="text-base font-semibold text-white tracking-tight">Gateway Overview</h2>
              <p class="text-xs text-neutral-400 mt-0.5">Users, team vaults, and live connected clients.</p>
            </div>
            <button
              type="button"
              onclick={() => {
                createUserError = null;
                isCreateUserOpen = true;
              }}
              class="inline-flex items-center justify-center gap-1.5 rounded-lg bg-emerald-600 hover:bg-emerald-500 px-3.5 py-2 text-xs font-semibold text-white transition cursor-pointer"
            >
              <UserPlus class="h-3.5 w-3.5" /> Add User
            </button>
          </div>

          <!-- Compact Stat Cards -->
          <div class="grid grid-cols-2 md:grid-cols-4 gap-3">
            <div class="rounded-xl border border-white/[0.06] bg-[#121217] p-4">
              <span class="text-[11px] text-neutral-400">Users</span>
              <div class="mt-1 text-2xl font-bold text-white">{users.length}</div>
            </div>

            <div class="rounded-xl border border-white/[0.06] bg-[#121217] p-4">
              <span class="text-[11px] text-neutral-400">Vaults</span>
              <div class="mt-1 text-2xl font-bold text-white">{vaults.length}</div>
              <p class="mt-0.5 text-[10px] text-neutral-500">{teamVaults.length} Team · {vaults.length - teamVaults.length} Personal</p>
            </div>

            <div class="rounded-xl border border-white/[0.06] bg-[#121217] p-4">
              <span class="text-[11px] text-neutral-400">Online Devices</span>
              <div class="mt-1 text-2xl font-bold text-emerald-400">{onlineDevices.length}</div>
            </div>

            <div class="rounded-xl border border-white/[0.06] bg-[#121217] p-4">
              <span class="text-[11px] text-neutral-400">Active SSH</span>
              <div class="mt-1 text-2xl font-bold text-white">{activeSshCount}</div>
            </div>
          </div>

          <!-- Live Online Presence -->
          <div class="rounded-xl border border-white/[0.06] bg-[#121217] p-4 space-y-3 text-xs">
            <div class="flex items-center justify-between">
              <h3 class="font-semibold text-white">Online Clients</h3>
              <button
                type="button"
                onclick={async () => { presenceList = await gatewayApi.getPresence().catch(() => []); }}
                class="text-[11px] text-emerald-400 hover:underline cursor-pointer"
              >
                Refresh
              </button>
            </div>

            {#if onlineDevices.length === 0}
              <p class="text-neutral-500 py-4 text-center">No client devices online right now.</p>
            {:else}
              <div class="divide-y divide-white/[0.05]">
                {#each onlineDevices as dev (dev.user_id + dev.device_id)}
                  <div class="py-2.5 flex flex-col sm:flex-row sm:items-center justify-between gap-2">
                    <div class="flex items-center gap-2.5 min-w-0">
                      <span class="h-2 w-2 rounded-full bg-emerald-400 shrink-0"></span>
                      <div class="min-w-0">
                        <p class="font-medium text-white truncate">
                          {dev.user_name}
                          <span class="text-neutral-500 font-normal">· {dev.user_email}</span>
                        </p>
                        <p class="text-[11px] text-neutral-400 capitalize">{dev.device_name} · {dev.platform}</p>
                      </div>
                    </div>
                    <div class="flex flex-wrap gap-1.5">
                      {#if dev.active_sessions.length === 0}
                        <span class="rounded-md bg-white/[0.04] px-2 py-0.5 text-[11px] text-neutral-400">Idle</span>
                      {:else}
                        {#each dev.active_sessions as sess (sess.session_id)}
                          <span class="rounded-md bg-emerald-500/12 border border-emerald-500/25 px-2 py-0.5 text-[11px] font-mono text-emerald-300">
                            {sess.host_label}
                          </span>
                        {/each}
                      {/if}
                    </div>
                  </div>
                {/each}
              </div>
            {/if}
          </div>
        </div>
      {:else if activeTab === 'users'}
        <div class="space-y-4 max-w-5xl">
          <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
            <div>
              <h2 class="text-base font-semibold text-white tracking-tight">Users</h2>
              <p class="text-xs text-neutral-400 mt-0.5">Invite users via email so they create their own password, or add manually.</p>
            </div>
            <div class="flex items-center gap-2">
              <button
                type="button"
                onclick={() => {
                  emailInviteError = null;
                  lastInviteLink = null;
                  isEmailInviteOpen = true;
                }}
                class="inline-flex items-center justify-center gap-1.5 px-3.5 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white font-semibold text-xs transition cursor-pointer"
              >
                <Mail class="h-3.5 w-3.5" /> Invite via Email
              </button>
              <button
                type="button"
                onclick={() => {
                  createUserError = null;
                  isCreateUserOpen = true;
                }}
                class="inline-flex items-center justify-center gap-1.5 px-3 py-2 rounded-lg border border-white/[0.08] bg-[#14141c] hover:bg-[#1b1b26] text-neutral-200 font-medium text-xs transition cursor-pointer"
              >
                <UserPlus class="h-3.5 w-3.5" /> Add Manually
              </button>
            </div>
          </div>

          <div class="relative max-w-xs">
            <Search class="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-neutral-500" />
            <input
              type="text"
              bind:value={userSearch}
              placeholder="Search users..."
              class="w-full rounded-lg border border-white/[0.07] bg-[#121217] pl-8 pr-3 py-1.5 text-xs text-white outline-none focus:border-emerald-500/60"
            />
          </div>

          <div class="rounded-xl border border-white/[0.06] bg-[#121217] overflow-x-auto text-xs">
            <table class="w-full text-left min-w-[560px]">
              <thead class="bg-white/[0.02] text-neutral-400 border-b border-white/[0.06] text-[11px]">
                <tr>
                  <th class="px-4 py-2.5 font-medium">Name</th>
                  <th class="px-4 py-2.5 font-medium">Email</th>
                  <th class="px-4 py-2.5 font-medium">Role</th>
                  <th class="px-4 py-2.5 font-medium">Created</th>
                  <th class="px-4 py-2.5 font-medium text-right">Actions</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-white/[0.05]">
                {#if filteredUsers.length === 0}
                  <tr>
                    <td colspan="5" class="p-6 text-center text-neutral-500">
                      {users.length === 0 ? 'No users yet.' : 'No matching users.'}
                    </td>
                  </tr>
                {:else}
                  {#each filteredUsers as u (u.id)}
                    <tr class="hover:bg-white/[0.02] transition">
                      <td class="px-4 py-2.5 font-medium text-white">{u.name}</td>
                      <td class="px-4 py-2.5 font-mono text-emerald-400">{u.email}</td>
                      <td class="px-4 py-2.5">
                        {#if u.role === 'admin' || u.id === currentUser?.id}
                          <span class="rounded bg-amber-500/15 border border-amber-500/30 px-2 py-0.5 text-[10px] font-semibold text-amber-300">ADMIN</span>
                        {:else}
                          <span class="rounded bg-white/[0.06] px-2 py-0.5 text-[10px] font-medium text-neutral-300">USER</span>
                        {/if}
                      </td>
                      <td class="px-4 py-2.5 text-neutral-400">{new Date(u.created_at).toLocaleDateString()}</td>
                      <td class="px-4 py-2.5 text-right">
                        {#if u.id !== currentUser?.id}
                          <button
                            type="button"
                            onclick={() => handleDeleteUser(u)}
                            class="inline-flex items-center gap-1 rounded-lg px-2 py-1 text-[11px] text-neutral-400 hover:text-rose-400 hover:bg-rose-500/10 transition cursor-pointer"
                          >
                            <Trash2 class="h-3.5 w-3.5" /> Delete
                          </button>
                        {:else}
                          <span class="text-[10px] text-neutral-500">You</span>
                        {/if}
                      </td>
                    </tr>
                  {/each}
                {/if}
              </tbody>
            </table>
          </div>

          {#if pendingInvites.length > 0}
            <div class="space-y-2 pt-2">
              <h3 class="text-xs font-semibold text-neutral-300">Pending Invites ({pendingInvites.length})</h3>
              <div class="rounded-xl border border-white/[0.06] bg-[#121217] overflow-x-auto text-xs">
                <table class="w-full text-left min-w-[480px]">
                  <thead class="bg-white/[0.02] text-neutral-400 border-b border-white/[0.06] text-[11px]">
                    <tr>
                      <th class="px-4 py-2 font-medium">Name</th>
                      <th class="px-4 py-2 font-medium">Email</th>
                      <th class="px-4 py-2 font-medium">Expires</th>
                      <th class="px-4 py-2 font-medium text-right">Actions</th>
                    </tr>
                  </thead>
                  <tbody class="divide-y divide-white/[0.05]">
                    {#each pendingInvites as inv (inv.id)}
                      <tr class="hover:bg-white/[0.02] transition">
                        <td class="px-4 py-2 font-medium text-white">{inv.name}</td>
                        <td class="px-4 py-2 font-mono text-amber-300">{inv.email}</td>
                        <td class="px-4 py-2 text-neutral-400">{new Date(inv.expires_at).toLocaleString()}</td>
                        <td class="px-4 py-2 text-right">
                          <div class="inline-flex items-center gap-1">
                            <button
                              type="button"
                              disabled={resendingInviteId === inv.id}
                              onclick={() => handleResendInvite(inv)}
                              class="inline-flex items-center gap-1 rounded-lg px-2 py-1 text-[11px] text-emerald-400 hover:bg-emerald-500/10 disabled:opacity-50 transition cursor-pointer"
                            >
                              <Send class="h-3.5 w-3.5" />
                              {resendingInviteId === inv.id ? 'Sending...' : 'Resend'}
                            </button>
                            <button
                              type="button"
                              onclick={() => handleRevokeInvite(inv)}
                              class="inline-flex items-center gap-1 rounded-lg px-2 py-1 text-[11px] text-neutral-400 hover:text-rose-400 hover:bg-rose-500/10 transition cursor-pointer"
                            >
                              <Trash2 class="h-3.5 w-3.5" /> Revoke
                            </button>
                          </div>
                        </td>
                      </tr>
                    {/each}
                  </tbody>
                </table>
              </div>
            </div>
          {/if}
        </div>
      {:else if activeTab === 'teams'}
        <div class="space-y-4 max-w-5xl">
          <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
            <div>
              <h2 class="text-base font-semibold text-white tracking-tight">Teams &amp; Vaults</h2>
              <p class="text-xs text-neutral-400 mt-0.5">Manage shared team vaults and member access.</p>
            </div>
            <div class="flex items-center gap-2">
              <button
                type="button"
                onclick={() => {
                  createVaultError = null;
                  isCreateVaultOpen = true;
                }}
                class="inline-flex items-center gap-1.5 px-3 py-2 rounded-lg border border-white/[0.08] bg-[#14141c] hover:bg-[#1b1b26] text-white font-medium text-xs transition cursor-pointer"
              >
                <FolderPlus class="h-3.5 w-3.5 text-emerald-400" /> New Vault
              </button>
              <button
                type="button"
                onclick={() => {
                  inviteError = null;
                  isInviteOpen = true;
                }}
                disabled={teamVaults.length === 0}
                class="inline-flex items-center gap-1.5 px-3 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white font-semibold text-xs transition disabled:opacity-40 cursor-pointer"
              >
                <UserPlus class="h-3.5 w-3.5" /> Add Member
              </button>
            </div>
          </div>

          {#if teamVaults.length > 0}
            <div class="flex items-center gap-2 flex-wrap">
              {#each teamVaults as tv (tv.id)}
                <button
                  type="button"
                  onclick={() => {
                    selectedTeamVaultId = tv.id;
                    loadVaultMembers(tv.id);
                  }}
                  class="rounded-lg px-3 py-1.5 text-xs font-medium border transition cursor-pointer {selectedTeamVaultId === tv.id ? 'bg-emerald-500/15 border-emerald-500/40 text-emerald-300' : 'bg-[#121217] border-white/[0.06] text-neutral-400 hover:text-white'}"
                >
                  {tv.name}
                </button>
              {/each}
            </div>
          {/if}

          <div class="rounded-xl border border-white/[0.06] bg-[#121217] overflow-x-auto text-xs">
            <table class="w-full text-left min-w-[460px]">
              <thead class="bg-white/[0.02] text-neutral-400 border-b border-white/[0.06] text-[11px]">
                <tr>
                  <th class="px-4 py-2.5 font-medium">Member</th>
                  <th class="px-4 py-2.5 font-medium">Role</th>
                  <th class="px-4 py-2.5 font-medium">Added</th>
                  <th class="px-4 py-2.5 font-medium text-right">Actions</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-white/[0.05]">
                {#if teamVaults.length === 0}
                  <tr>
                    <td colspan="4" class="p-6 text-center text-neutral-500">
                      No team vaults yet. Click "New Vault" to create one.
                    </td>
                  </tr>
                {:else if teamMembers.length === 0}
                  <tr>
                    <td colspan="4" class="p-6 text-center text-neutral-500">
                      No members in this vault yet.
                    </td>
                  </tr>
                {:else}
                  {#each teamMembers as member (member.id)}
                    <tr class="hover:bg-white/[0.02] transition">
                      <td class="px-4 py-2.5">
                        {#if member.name || member.email}
                          <p class="font-medium text-white">{member.name || member.email}</p>
                          {#if member.name && member.email}
                            <p class="font-mono text-[11px] text-neutral-400">{member.email}</p>
                          {/if}
                        {:else}
                          <span class="font-mono text-white">{member.user_id}</span>
                        {/if}
                      </td>
                      <td class="px-4 py-2.5">
                        <span class="rounded px-2 py-0.5 text-[10px] font-semibold uppercase {member.role === 'owner' ? 'bg-amber-500/15 text-amber-300 border border-amber-500/30' : 'bg-emerald-500/12 text-emerald-300 border border-emerald-500/25'}">
                          {member.role}
                        </span>
                      </td>
                      <td class="px-4 py-2.5 text-neutral-400">{new Date(member.created_at).toLocaleDateString()}</td>
                      <td class="px-4 py-2.5 text-right">
                        {#if member.role !== 'owner'}
                          <button
                            type="button"
                            onclick={() => handleRemoveVaultMember(member)}
                            class="inline-flex items-center gap-1 rounded-lg px-2 py-1 text-[11px] text-neutral-400 hover:text-rose-400 hover:bg-rose-500/10 transition cursor-pointer"
                          >
                            <Trash2 class="h-3.5 w-3.5" /> Remove
                          </button>
                        {:else}
                          <span class="text-[10px] text-neutral-500">Owner</span>
                        {/if}
                      </td>
                    </tr>
                  {/each}
                {/if}
              </tbody>
            </table>
          </div>
        </div>
      {:else if activeTab === 'settings'}
        <div class="space-y-5 max-w-2xl text-xs">
          <div>
            <h2 class="text-base font-semibold text-white tracking-tight">Settings</h2>
            <p class="text-xs text-neutral-400 mt-0.5">Admin account credentials, Gateway API, and SMTP email delivery.</p>
          </div>

          <!-- Admin Account Security (Change Name / Email / Password) -->
          <form
            onsubmit={handleSaveProfile}
            class="rounded-xl border border-white/[0.06] bg-[#121217] p-4 space-y-3.5"
          >
            <div>
              <h3 class="text-xs font-semibold text-white flex items-center gap-1.5">
                <Lock class="h-3.5 w-3.5 text-emerald-400" /> Admin Account &amp; Password
              </h3>
              <p class="text-[11px] text-neutral-400 mt-0.5">Change your admin display name, login email, or password. Your Admin role stays preserved.</p>
            </div>

            {#if profileError}
              <div class="rounded-lg bg-rose-950/40 border border-rose-500/30 p-2.5 text-rose-300 text-[11px]">
                {profileError}
              </div>
            {/if}

            <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div>
                <label for="prof-name" class="block font-medium text-neutral-400 mb-1">Display Name</label>
                <input
                  id="prof-name"
                  type="text"
                  required
                  bind:value={profileName}
                  class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white outline-none focus:border-emerald-500/70"
                />
              </div>
              <div>
                <label for="prof-email" class="block font-medium text-neutral-400 mb-1">Admin Email</label>
                <input
                  id="prof-email"
                  type="email"
                  required
                  bind:value={profileEmail}
                  class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white font-mono outline-none focus:border-emerald-500/70"
                />
              </div>
            </div>

            <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div>
                <label for="prof-new-pw" class="block font-medium text-neutral-400 mb-1">New Password (optional)</label>
                <input
                  id="prof-new-pw"
                  type="password"
                  autocomplete="new-password"
                  bind:value={profileNewPassword}
                  placeholder="Leave blank to keep current"
                  class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white outline-none focus:border-emerald-500/70"
                />
              </div>
              <div>
                <label for="prof-confirm-pw" class="block font-medium text-neutral-400 mb-1">Confirm New Password</label>
                <input
                  id="prof-confirm-pw"
                  type="password"
                  autocomplete="new-password"
                  bind:value={profileConfirmPassword}
                  placeholder="Repeat new password"
                  class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white outline-none focus:border-emerald-500/70"
                />
              </div>
            </div>

            <div class="flex flex-col sm:flex-row sm:items-end gap-3 pt-1">
              <div class="flex-1">
                <label for="prof-cur-pw" class="block font-medium text-neutral-400 mb-1">Current Password *</label>
                <input
                  id="prof-cur-pw"
                  type="password"
                  required
                  autocomplete="current-password"
                  bind:value={profileCurrentPassword}
                  placeholder="Required to confirm changes"
                  class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white outline-none focus:border-emerald-500/70"
                />
              </div>
              <button
                type="submit"
                disabled={isSavingProfile}
                class="px-4 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white font-semibold transition disabled:opacity-50 cursor-pointer shrink-0"
              >
                {isSavingProfile ? 'Updating...' : 'Update Account'}
              </button>
            </div>
          </form>

          <form
            onsubmit={(e) => {
              e.preventDefault();
              gatewayApi.setApiBase(backendUrl.trim());
              showBanner('Gateway API endpoint saved.');
              void refreshData();
            }}
            class="rounded-xl border border-white/[0.06] bg-[#121217] p-4 space-y-3"
          >
            <h3 class="text-xs font-semibold text-white">Gateway REST API</h3>
            <div>
              <label for="set-api" class="block font-medium text-neutral-400 mb-1">API Base URL</label>
              <input
                id="set-api"
                type="url"
                bind:value={backendUrl}
                class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white font-mono outline-none focus:border-emerald-500/70"
              />
            </div>
            <button
              type="submit"
              class="px-4 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white font-semibold transition cursor-pointer"
            >
              Save Endpoint
            </button>
          </form>

          <!-- SMTP Configuration (Stored in PostgreSQL) -->
          <form
            onsubmit={handleSaveSmtp}
            class="rounded-xl border border-white/[0.06] bg-[#121217] p-4 space-y-4"
          >
            <div class="flex items-center justify-between gap-3">
              <div>
                <h3 class="text-xs font-semibold text-white flex items-center gap-1.5">
                  <Mail class="h-3.5 w-3.5 text-emerald-400" /> SMTP Email Configuration
                </h3>
                <p class="text-[11px] text-neutral-400 mt-0.5">Used for team invite links (self-service password setup) and vault notifications.</p>
              </div>
              <label class="inline-flex items-center gap-2 cursor-pointer shrink-0">
                <input
                  type="checkbox"
                  bind:checked={smtpConfig.enabled}
                  class="h-4 w-4 accent-emerald-500 rounded cursor-pointer"
                />
                <span class="text-xs font-medium {smtpConfig.enabled ? 'text-emerald-400' : 'text-neutral-400'}">
                  {smtpConfig.enabled ? 'Enabled' : 'Disabled'}
                </span>
              </label>
            </div>

            {#if smtpError}
              <div class="rounded-lg bg-rose-950/40 border border-rose-500/30 p-2.5 text-rose-300 text-[11px]">
                {smtpError}
              </div>
            {/if}

            <div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
              <div class="sm:col-span-2">
                <label for="smtp-host" class="block font-medium text-neutral-400 mb-1">SMTP Host</label>
                <input
                  id="smtp-host"
                  type="text"
                  bind:value={smtpConfig.host}
                  placeholder="smtp.gmail.com"
                  class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white font-mono outline-none focus:border-emerald-500/70"
                />
              </div>
              <div>
                <label for="smtp-port" class="block font-medium text-neutral-400 mb-1">Port</label>
                <input
                  id="smtp-port"
                  type="number"
                  bind:value={smtpConfig.port}
                  placeholder="587"
                  class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white font-mono outline-none focus:border-emerald-500/70"
                />
              </div>
            </div>

            <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div>
                <label for="smtp-user" class="block font-medium text-neutral-400 mb-1">SMTP Username</label>
                <input
                  id="smtp-user"
                  type="text"
                  bind:value={smtpConfig.username}
                  placeholder="noreply@yourdomain.com"
                  class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white outline-none focus:border-emerald-500/70"
                />
              </div>
              <div>
                <label for="smtp-pass" class="block font-medium text-neutral-400 mb-1">SMTP Password / App Password</label>
                <input
                  id="smtp-pass"
                  type="password"
                  bind:value={smtpConfig.password}
                  placeholder={smtpConfig.has_password ? 'Saved — leave blank to keep' : 'Enter SMTP password'}
                  class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white outline-none focus:border-emerald-500/70"
                />
              </div>
            </div>

            <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div>
                <label for="smtp-from-name" class="block font-medium text-neutral-400 mb-1">From Name</label>
                <input
                  id="smtp-from-name"
                  type="text"
                  bind:value={smtpConfig.from_name}
                  placeholder="OpenTermius Gateway"
                  class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white outline-none focus:border-emerald-500/70"
                />
              </div>
              <div>
                <label for="smtp-from-email" class="block font-medium text-neutral-400 mb-1">From Email</label>
                <input
                  id="smtp-from-email"
                  type="email"
                  bind:value={smtpConfig.from_email}
                  placeholder="noreply@yourdomain.com"
                  class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white outline-none focus:border-emerald-500/70"
                />
              </div>
            </div>

            <div>
              <label for="smtp-web-url" class="block font-medium text-neutral-400 mb-1">Gateway Web URL (for Invite Links)</label>
              <input
                id="smtp-web-url"
                type="url"
                bind:value={smtpConfig.web_base_url}
                placeholder="https://gateway.yourdomain.com"
                class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white font-mono outline-none focus:border-emerald-500/70"
              />
            </div>

            <div class="flex flex-wrap items-center justify-between gap-3 pt-1">
              <label class="inline-flex items-center gap-2 text-neutral-300 cursor-pointer">
                <input
                  type="checkbox"
                  bind:checked={smtpConfig.use_tls}
                  class="h-4 w-4 accent-emerald-500 rounded cursor-pointer"
                />
                <span>Use TLS / STARTTLS (recommended)</span>
              </label>

              <button
                type="submit"
                disabled={isSavingSmtp}
                class="px-4 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white font-semibold transition disabled:opacity-50 cursor-pointer"
              >
                {isSavingSmtp ? 'Saving...' : 'Save SMTP Settings'}
              </button>
            </div>
          </form>

          <!-- Test SMTP Email -->
          <form
            onsubmit={handleTestSmtp}
            class="rounded-xl border border-white/[0.06] bg-[#121217] p-4 flex flex-col sm:flex-row sm:items-end gap-3"
          >
            <div class="flex-1">
              <label for="smtp-test-to" class="block font-medium text-neutral-400 mb-1">Send Test Email</label>
              <input
                id="smtp-test-to"
                type="email"
                bind:value={testEmailTo}
                placeholder={currentUser?.email || 'you@yourdomain.com'}
                class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white outline-none focus:border-emerald-500/70"
              />
            </div>
            <button
              type="submit"
              disabled={isTestingSmtp}
              class="inline-flex items-center justify-center gap-1.5 px-4 py-2 rounded-lg border border-white/[0.1] bg-[#181822] hover:bg-[#222230] text-white font-medium transition disabled:opacity-50 cursor-pointer shrink-0"
            >
              <Send class="h-3.5 w-3.5 text-emerald-400" />
              <span>{isTestingSmtp ? 'Sending...' : 'Send Test'}</span>
            </button>
          </form>
        </div>
      {/if}
    </div>
  </main>
</div>

<!-- Add User Modal -->
{#if isCreateUserOpen}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/75 backdrop-blur-xs p-4" onclick={(e) => { if (e.target === e.currentTarget) isCreateUserOpen = false; }} role="presentation">
    <form
      onsubmit={handleCreateUser}
      class="w-full max-w-md rounded-2xl border border-white/[0.08] bg-[#13131b] p-5 shadow-2xl space-y-4 text-xs"
    >
      <div class="flex items-center justify-between border-b border-white/[0.06] pb-3">
        <h3 class="text-sm font-semibold text-white">Add User Account</h3>
        <button type="button" onclick={() => (isCreateUserOpen = false)} class="text-neutral-400 hover:text-white">
          <X class="h-4 w-4" />
        </button>
      </div>

      {#if createUserError}
        <div class="rounded-lg bg-rose-950/40 border border-rose-500/30 p-2.5 text-rose-300 text-[11px]">
          {createUserError}
        </div>
      {/if}

      <div class="space-y-3">
        <div>
          <label for="new-user-name" class="block text-neutral-400 font-medium mb-1">Full Name</label>
          <input
            id="new-user-name"
            type="text"
            required
            bind:value={newUserName}
            placeholder="Budi Santoso"
            class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white outline-none focus:border-emerald-500/70"
          />
        </div>
        <div>
          <label for="new-user-email" class="block text-neutral-400 font-medium mb-1">Email</label>
          <input
            id="new-user-email"
            type="email"
            required
            bind:value={newUserEmail}
            placeholder="budi@company.com"
            class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white outline-none focus:border-emerald-500/70"
          />
        </div>
        <div>
          <label for="new-user-pw" class="block text-neutral-400 font-medium mb-1">Initial Password</label>
          <input
            id="new-user-pw"
            type="password"
            required
            minlength="8"
            bind:value={newUserPassword}
            placeholder="Minimum 8 characters"
            class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white outline-none focus:border-emerald-500/70"
          />
        </div>
      </div>

      <div class="flex justify-end gap-2 pt-3 border-t border-white/[0.06]">
        <button type="button" onclick={() => (isCreateUserOpen = false)} class="px-3 py-1.5 rounded-lg border border-white/[0.08] text-neutral-400 hover:text-white transition">
          Cancel
        </button>
        <button type="submit" disabled={isCreatingUser} class="px-4 py-1.5 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white font-semibold transition disabled:opacity-50 cursor-pointer">
          {isCreatingUser ? 'Creating...' : 'Create User'}
        </button>
      </div>
    </form>
  </div>
{/if}

<!-- Create Team Vault Modal -->
{#if isCreateVaultOpen}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/75 backdrop-blur-xs p-4" onclick={(e) => { if (e.target === e.currentTarget) isCreateVaultOpen = false; }} role="presentation">
    <form
      onsubmit={handleCreateTeamVault}
      class="w-full max-w-md rounded-2xl border border-white/[0.08] bg-[#13131b] p-5 shadow-2xl space-y-4 text-xs"
    >
      <div class="flex items-center justify-between border-b border-white/[0.06] pb-3">
        <h3 class="text-sm font-semibold text-white">New Team Vault</h3>
        <button type="button" onclick={() => (isCreateVaultOpen = false)} class="text-neutral-400 hover:text-white">
          <X class="h-4 w-4" />
        </button>
      </div>

      {#if createVaultError}
        <div class="rounded-lg bg-rose-950/40 border border-rose-500/30 p-2.5 text-rose-300 text-[11px]">
          {createVaultError}
        </div>
      {/if}

      <div>
        <label for="new-vault-name" class="block text-neutral-400 font-medium mb-1">Vault Name</label>
        <input
          id="new-vault-name"
          type="text"
          required
          bind:value={newVaultName}
          placeholder="Production Servers"
          class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white outline-none focus:border-emerald-500/70"
        />
      </div>

      <div class="flex justify-end gap-2 pt-3 border-t border-white/[0.06]">
        <button type="button" onclick={() => (isCreateVaultOpen = false)} class="px-3 py-1.5 rounded-lg border border-white/[0.08] text-neutral-400 hover:text-white transition">
          Cancel
        </button>
        <button type="submit" disabled={isCreatingVault} class="px-4 py-1.5 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white font-semibold transition disabled:opacity-50 cursor-pointer">
          {isCreatingVault ? 'Creating...' : 'Create Vault'}
        </button>
      </div>
    </form>
  </div>
{/if}

<!-- Invite Member Modal -->
{#if isInviteOpen}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/75 backdrop-blur-xs p-4" onclick={(e) => { if (e.target === e.currentTarget) isInviteOpen = false; }} role="presentation">
    <form
      onsubmit={handleInviteMember}
      class="w-full max-w-md rounded-2xl border border-white/[0.08] bg-[#13131b] p-5 shadow-2xl space-y-4 text-xs"
    >
      <div class="flex items-center justify-between border-b border-white/[0.06] pb-3">
        <h3 class="text-sm font-semibold text-white">Add Member to Team Vault</h3>
        <button type="button" onclick={() => (isInviteOpen = false)} class="text-neutral-400 hover:text-white">
          <X class="h-4 w-4" />
        </button>
      </div>

      {#if inviteError}
        <div class="rounded-lg bg-rose-950/40 border border-rose-500/30 p-2.5 text-rose-300 text-[11px]">
          {inviteError}
        </div>
      {/if}

      <div class="space-y-3">
        <div>
          <label for="inv-email" class="block text-neutral-400 font-medium mb-1">User Email</label>
          <input
            id="inv-email"
            type="email"
            required
            list="registered-users-list"
            bind:value={inviteEmail}
            placeholder="colleague@company.com"
            class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white outline-none focus:border-emerald-500/70"
          />
          <datalist id="registered-users-list">
            {#each users as u (u.id)}
              <option value={u.email}>{u.name}</option>
            {/each}
          </datalist>
        </div>

        <div>
          <label for="inv-role" class="block text-neutral-400 font-medium mb-1">Role</label>
          <select
            id="inv-role"
            bind:value={inviteRole}
            class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white outline-none focus:border-emerald-500/70"
          >
            <option value="member">Member (Read &amp; Connect)</option>
            <option value="admin">Admin (Manage Hosts &amp; Keys)</option>
          </select>
        </div>
      </div>

      <div class="flex justify-end gap-2 pt-3 border-t border-white/[0.06]">
        <button type="button" onclick={() => (isInviteOpen = false)} class="px-3 py-1.5 rounded-lg border border-white/[0.08] text-neutral-400 hover:text-white transition">
          Cancel
        </button>
        <button type="submit" disabled={isInviting} class="px-4 py-1.5 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white font-semibold transition disabled:opacity-50 cursor-pointer">
          {isInviting ? 'Adding...' : 'Add Member'}
        </button>
      </div>
    </form>
  </div>
{/if}

<!-- Invite User via Email Modal (Self-Service Password Setup) -->
{#if isEmailInviteOpen}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/75 backdrop-blur-xs p-4" onclick={(e) => { if (e.target === e.currentTarget) isEmailInviteOpen = false; }} role="presentation">
    <form
      onsubmit={handleSendEmailInvite}
      class="w-full max-w-md rounded-2xl border border-white/[0.08] bg-[#13131b] p-5 shadow-2xl space-y-4 text-xs"
    >
      <div class="flex items-center justify-between border-b border-white/[0.06] pb-3">
        <h3 class="text-sm font-semibold text-white">Invite User via Email</h3>
        <button type="button" onclick={() => (isEmailInviteOpen = false)} class="text-neutral-400 hover:text-white">
          <X class="h-4 w-4" />
        </button>
      </div>

      <p class="text-[11px] text-neutral-400">
        The invited user receives a one-time link (valid 24 hours) to create their own password.
      </p>

      {#if emailInviteError}
        <div class="rounded-lg bg-rose-950/40 border border-rose-500/30 p-2.5 text-rose-300 text-[11px]">
          {emailInviteError}
        </div>
      {/if}

      {#if lastInviteLink}
        <div class="rounded-lg border border-emerald-500/30 bg-emerald-950/25 p-3 space-y-2">
          <div class="flex items-center justify-between">
            <span class="text-[11px] font-medium text-emerald-300">Invite Link Ready</span>
            <button
              type="button"
              onclick={() => copyInviteUrl(lastInviteLink!)}
              class="inline-flex items-center gap-1 rounded px-2 py-0.5 text-[11px] bg-emerald-500/20 text-emerald-300 hover:bg-emerald-500/30 cursor-pointer"
            >
              {#if copiedInviteLink}
                <Check class="h-3 w-3" /> Copied
              {:else}
                <Copy class="h-3 w-3" /> Copy Link
              {/if}
            </button>
          </div>
          <p class="font-mono text-[10px] text-neutral-300 break-all">{lastInviteLink}</p>
        </div>
      {/if}

      <div class="space-y-3">
        <div>
          <label for="ei-name" class="block text-neutral-400 font-medium mb-1">Full Name</label>
          <input
            id="ei-name"
            type="text"
            required
            bind:value={emailInviteName}
            placeholder="Budi Santoso"
            class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white outline-none focus:border-emerald-500/70"
          />
        </div>
        <div>
          <label for="ei-email" class="block text-neutral-400 font-medium mb-1">Email</label>
          <input
            id="ei-email"
            type="email"
            required
            bind:value={emailInviteAddr}
            placeholder="budi@company.com"
            class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white outline-none focus:border-emerald-500/70"
          />
        </div>
      </div>

      <div class="flex justify-end gap-2 pt-3 border-t border-white/[0.06]">
        <button type="button" onclick={() => (isEmailInviteOpen = false)} class="px-3 py-1.5 rounded-lg border border-white/[0.08] text-neutral-400 hover:text-white transition">
          Close
        </button>
        <button type="submit" disabled={isSendingInvite} class="inline-flex items-center gap-1.5 px-4 py-1.5 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white font-semibold transition disabled:opacity-50 cursor-pointer">
          <Send class="h-3.5 w-3.5" />
          <span>{isSendingInvite ? 'Sending...' : 'Send Invite'}</span>
        </button>
      </div>
    </form>
  </div>
{/if}

{#if confirmDialog}
  <div
    class="fixed inset-0 z-[70] flex items-center justify-center bg-black/75 backdrop-blur-xs p-4"
    onclick={(e) => { if (e.target === e.currentTarget) confirmDialog = null; }}
    role="presentation"
  >
    <div class="w-full max-w-sm rounded-2xl border border-white/[0.08] bg-[#13131b] p-5 shadow-2xl space-y-4 text-xs">
      <div class="flex items-start gap-3">
        <div class="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-rose-500/15 text-rose-400 border border-rose-500/25">
          <AlertCircle class="h-4.5 w-4.5" />
        </div>
        <div class="space-y-1">
          <h3 class="text-sm font-semibold text-white">{confirmDialog.title}</h3>
          <p class="text-neutral-300 leading-relaxed">{confirmDialog.message}</p>
        </div>
      </div>
      <div class="flex justify-end gap-2 pt-2 border-t border-white/[0.06]">
        <button
          type="button"
          onclick={() => (confirmDialog = null)}
          class="px-3.5 py-1.5 rounded-lg border border-white/[0.08] text-neutral-300 hover:text-white transition cursor-pointer"
        >
          Cancel
        </button>
        <button
          type="button"
          onclick={async () => {
            const fn = confirmDialog?.onConfirm;
            confirmDialog = null;
            if (fn) await fn();
          }}
          class="px-4 py-1.5 rounded-lg bg-rose-600 hover:bg-rose-500 text-white font-semibold transition cursor-pointer"
        >
          {confirmDialog.confirmLabel}
        </button>
      </div>
    </div>
  </div>
{/if}
{/if}
