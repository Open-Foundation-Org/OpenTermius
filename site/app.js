(function () {
  const DEFAULT_VERSION = '0.7.1';
  const REPO = 'Open-Foundation-Org/OpenTermius';
  const RELEASE_BASE = `https://github.com/${REPO}/releases/download/v${DEFAULT_VERSION}`;

  const fallbackAssets = {
    version: DEFAULT_VERSION,
    mac: `${RELEASE_BASE}/OpenTermius_${DEFAULT_VERSION}_macOS_aarch64.dmg`,
    win: `${RELEASE_BASE}/OpenTermius_${DEFAULT_VERSION}_windows_x64-setup.exe`,
    android: `${RELEASE_BASE}/OpenTermius_${DEFAULT_VERSION}_android_universal.apk`
  };

  function detectPlatform() {
    const ua = navigator.userAgent || '';
    if (/android/i.test(ua)) return 'android';
    if (/windows|win32|win64/i.test(ua)) return 'win';
    if (/macintosh|mac os x/i.test(ua) && !/iphone|ipad|ipod/i.test(ua)) return 'mac';
    return 'other';
  }

  function applyDownloadLinks(assets) {
    const heroBtn = document.getElementById('hero-download-btn');
    const heroLabel = document.getElementById('hero-download-label');
    const macLink = document.getElementById('dl-mac');
    const winLink = document.getElementById('dl-win');
    const androidLink = document.getElementById('dl-android');

    if (macLink) macLink.href = assets.mac;
    if (winLink) winLink.href = assets.win;
    if (androidLink) androidLink.href = assets.android;

    const platform = detectPlatform();
    if (heroBtn && heroLabel) {
      if (platform === 'mac') {
        heroBtn.href = assets.mac;
        heroLabel.textContent = `Download v${assets.version} for macOS`;
      } else if (platform === 'win') {
        heroBtn.href = assets.win;
        heroLabel.textContent = `Download v${assets.version} for Windows`;
      } else if (platform === 'android') {
        heroBtn.href = assets.android;
        heroLabel.textContent = `Download v${assets.version} for Android`;
      } else {
        heroBtn.href = '#download';
        heroLabel.textContent = `Download OpenTermius v${assets.version}`;
      }
    }
  }

  applyDownloadLinks(fallbackAssets);

  // Hydrate direct installer URLs from GitHub Releases API
  fetch(`https://api.github.com/repos/${REPO}/releases/latest`)
    .then((res) => (res.ok ? res.json() : null))
    .then((data) => {
      if (!data || !Array.isArray(data.assets)) return;
      const ver = String(data.tag_name || '').replace(/^v/i, '') || DEFAULT_VERSION;
      const findUrl = (ext) =>
        data.assets.find((a) => typeof a.name === 'string' && a.name.toLowerCase().endsWith(ext))
          ?.browser_download_url;

      applyDownloadLinks({
        version: ver,
        mac: findUrl('.dmg') || fallbackAssets.mac,
        win: findUrl('.exe') || fallbackAssets.win,
        android: findUrl('.apk') || fallbackAssets.android
      });
    })
    .catch(() => {
      // Keep v0.7.0 fallback URLs when offline or rate-limited
    });

  // Sticky Header Separation on Scroll
  const header = document.getElementById('site-header');
  if (header) {
    const syncScroll = () => {
      header.classList.toggle('scrolled', window.scrollY > 12);
    };
    syncScroll();
    window.addEventListener('scroll', syncScroll, { passive: true });
  }

  // Interactive Showcase Tabs
  const tabBtns = Array.from(document.querySelectorAll('[data-tab]'));
  const panes = Array.from(document.querySelectorAll('[data-pane]'));
  const toggleBtn = document.getElementById('showcase-toggle');
  const statusEl = document.getElementById('showcase-status');
  const showcaseEl = document.getElementById('showcase');

  const statusLabels = [
    'Team Live SSH: 1-click Remote Control and Rejoin over WebSocket relay',
    'Native Rust engine: Ed25519 keygen, local/SOCKS5 tunnels, and SFTP',
    'Gateway Admin Console: manage users, team vaults, and active devices',
    'Android app: foreground keep-alive, Focus Mode, and 2x7 extra keys'
  ];

  let activeIdx = 0;
  let playing = !window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  let inViewport = false;
  let timer = null;

  function showTab(idx) {
    activeIdx = idx;
    tabBtns.forEach((btn, i) => {
      const isMatch = i === idx;
      btn.classList.toggle('active', isMatch);
      btn.setAttribute('aria-selected', String(isMatch));
    });
    panes.forEach((pane, i) => {
      pane.classList.toggle('active', i === idx);
    });
    if (statusEl && statusLabels[idx]) {
      statusEl.textContent = statusLabels[idx];
    }
  }

  function syncTimer() {
    if (timer) {
      clearInterval(timer);
      timer = null;
    }
    if (playing && inViewport && tabBtns.length > 0) {
      timer = setInterval(() => {
        showTab((activeIdx + 1) % tabBtns.length);
      }, 6000);
    }
  }

  tabBtns.forEach((btn) => {
    btn.addEventListener('click', () => {
      const idx = Number(btn.getAttribute('data-tab'));
      showTab(idx);
      syncTimer();
    });
  });

  if (toggleBtn) {
    toggleBtn.textContent = playing ? 'Pause preview' : 'Auto-play';
    toggleBtn.addEventListener('click', () => {
      playing = !playing;
      toggleBtn.textContent = playing ? 'Pause preview' : 'Auto-play';
      syncTimer();
    });
  }

  if (showcaseEl && 'IntersectionObserver' in window) {
    const stageObserver = new IntersectionObserver(
      (entries) => {
        inViewport = entries[0]?.isIntersecting ?? false;
        syncTimer();
      },
      { threshold: 0.2 }
    );
    stageObserver.observe(showcaseEl);
  }

  // Copy-to-Clipboard Buttons
  document.querySelectorAll('[data-copy]').forEach((btn) => {
    btn.addEventListener('click', () => {
      const targetId = btn.getAttribute('data-copy');
      const codeEl = targetId ? document.getElementById(targetId) : null;
      if (!codeEl) return;
      navigator.clipboard.writeText(codeEl.textContent.trim()).then(() => {
        const prev = btn.textContent;
        btn.textContent = 'Copied';
        setTimeout(() => {
          btn.textContent = prev;
        }, 1600);
      });
    });
  });

  // Scroll Reveal
  if ('IntersectionObserver' in window) {
    const revealObserver = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          if (entry.isIntersecting) {
            entry.target.classList.add('in-view');
            revealObserver.unobserve(entry.target);
          }
        });
      },
      { threshold: 0.1 }
    );
    document.querySelectorAll('.reveal').forEach((el) => revealObserver.observe(el));
  } else {
    document.querySelectorAll('.reveal').forEach((el) => el.classList.add('in-view'));
  }
})();
