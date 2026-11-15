// Reactive Platform & Device Form-Factor Detection (Svelte 5 Runes)

export type LayoutMode = 'mobile' | 'tablet' | 'desktop';

class PlatformStore {
  width = $state<number>(typeof window !== 'undefined' ? window.innerWidth : 1280);
  height = $state<number>(typeof window !== 'undefined' ? window.innerHeight : 800);
  maxHeight = $state<number>(typeof window !== 'undefined' ? window.innerHeight : 800);
  visualHeight = $state<number>(
    typeof window !== 'undefined' ? (window.visualViewport?.height ?? window.innerHeight) : 800
  );
  userAgent = $state<string>(typeof navigator !== 'undefined' ? navigator.userAgent : '');
  isTouch = $state<boolean>(
    typeof window !== 'undefined' &&
      ('ontouchstart' in window || navigator.maxTouchPoints > 0)
  );
  isInputFocused = $state<boolean>(false);

  isAndroid = $derived(/android/i.test(this.userAgent));
  isIOS = $derived(/ipad|iphone|ipod/i.test(this.userAgent));
  isMac = $derived(/macintosh|mac os x/i.test(this.userAgent) && !this.isIOS);
  isNativeTauri = $derived(
    typeof window !== 'undefined' && '__TAURI_INTERNALS__' in window
  );

  layoutMode = $derived.by<LayoutMode>(() => {
    if (this.width < 768) return 'mobile';
    if (this.width < 1024 || (this.isTouch && this.width <= 1180 && (this.isAndroid || this.isIOS))) {
      return 'tablet';
    }
    return 'desktop';
  });

  isMobile = $derived(this.layoutMode === 'mobile');
  isTablet = $derived(this.layoutMode === 'tablet');
  isDesktop = $derived(this.layoutMode === 'desktop');
  isHandheld = $derived(this.layoutMode === 'mobile' || this.layoutMode === 'tablet');
  isKeyboardOpen = $derived(
    this.isHandheld &&
      ((this.maxHeight > 0 && this.visualHeight < this.maxHeight * 0.8) ||
        (this.height > 0 && this.visualHeight < this.height * 0.8))
  );

  constructor() {
    if (typeof window !== 'undefined') {
      const update = () => {
        const w = window.innerWidth;
        const h = window.innerHeight;
        if (Math.abs(w - this.width) > 120) {
          // Orientation changed: reset baseline maxHeight
          this.maxHeight = h;
        } else if (h > this.maxHeight) {
          this.maxHeight = h;
        }
        this.width = w;
        this.height = h;
        this.visualHeight = Math.min(h, window.visualViewport?.height ?? h);
        this.userAgent = navigator.userAgent;
        this.isTouch = 'ontouchstart' in window || navigator.maxTouchPoints > 0;
      };
      const updateViewport = () => {
        const h = window.innerHeight;
        if (h > this.maxHeight) this.maxHeight = h;
        this.height = h;
        this.visualHeight = Math.min(h, window.visualViewport?.height ?? h);
      };
      window.addEventListener('resize', update);
      window.addEventListener('orientationchange', () => {
        setTimeout(() => {
          this.maxHeight = window.innerHeight;
          update();
        }, 120);
      });
      window.visualViewport?.addEventListener('resize', updateViewport);
      window.addEventListener('focusin', (e) => {
        const tag = (e.target as HTMLElement | null)?.tagName;
        if (tag === 'INPUT' || tag === 'TEXTAREA') {
          this.isInputFocused = true;
        }
      });
      window.addEventListener('focusout', () => {
        setTimeout(() => {
          const tag = document.activeElement?.tagName;
          this.isInputFocused = tag === 'INPUT' || tag === 'TEXTAREA';
        }, 100);
      });
    }
  }
}

export const platform = new PlatformStore();
