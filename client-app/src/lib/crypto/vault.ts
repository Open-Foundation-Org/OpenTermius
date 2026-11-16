// OpenTermius Client-Side WebCrypto E2EE Adapter (AES-256-GCM + PBKDF2)

const KEY_CACHE = new Map<string, CryptoKey>();
const FALLBACK_SECRET_KEY = 'opentermius-e2ee-default-vault-secret';
const APP_SALT = new TextEncoder().encode('OpenTermius-E2EE-Salt-v1');

export async function deriveKeyFromPassword(password: string, salt: Uint8Array): Promise<CryptoKey> {
  const enc = new TextEncoder();
  const keyMaterial = await window.crypto.subtle.importKey(
    'raw',
    enc.encode(password),
    'PBKDF2',
    false,
    ['deriveBits', 'deriveKey']
  );

  return window.crypto.subtle.deriveKey(
    {
      name: 'PBKDF2',
      salt: salt as BufferSource,
      iterations: 100000,
      hash: 'SHA-256'
    },
    keyMaterial,
    { name: 'AES-GCM', length: 256 },
    true,
    ['encrypt', 'decrypt']
  );
}

async function getCachedVaultKey(vaultSecret?: string): Promise<CryptoKey | null> {
  if (typeof window === 'undefined' || !window.crypto?.subtle) return null;
  const secret = vaultSecret?.trim() || FALLBACK_SECRET_KEY;
  const cached = KEY_CACHE.get(secret);
  if (cached) return cached;
  const key = await deriveKeyFromPassword(secret, APP_SALT);
  KEY_CACHE.set(secret, key);
  return key;
}

export async function encryptVaultItem(
  payload: unknown,
  vaultKey: CryptoKey
): Promise<{ ciphertext: string; nonce: string }> {
  const enc = new TextEncoder();
  const jsonBytes = enc.encode(JSON.stringify(payload));

  // Generate 96-bit (12-byte) initialization vector
  const iv = window.crypto.getRandomValues(new Uint8Array(12));

  const encryptedBuffer = await window.crypto.subtle.encrypt(
    {
      name: 'AES-GCM',
      iv
    },
    vaultKey,
    jsonBytes
  );

  return {
    ciphertext: bufferToBase64(new Uint8Array(encryptedBuffer)),
    nonce: bufferToBase64(iv)
  };
}

export async function decryptVaultItem<T>(
  ciphertextBase64: string,
  nonceBase64: string,
  vaultKey: CryptoKey
): Promise<T> {
  const iv = base64ToBuffer(nonceBase64);
  const encryptedBytes = base64ToBuffer(ciphertextBase64);

  const decryptedBuffer = await window.crypto.subtle.decrypt(
    {
      name: 'AES-GCM',
      iv: iv as BufferSource
    },
    vaultKey,
    encryptedBytes as BufferSource
  );

  const dec = new TextDecoder();
  return JSON.parse(dec.decode(decryptedBuffer)) as T;
}

export async function encryptVaultPayload(
  payload: unknown,
  vaultSecret?: string
): Promise<{ ciphertext: string; nonce: string }> {
  const key = await getCachedVaultKey(vaultSecret);
  if (key) {
    return encryptVaultItem(payload, key);
  }
  // Fallback only if WebCrypto subtle is unavailable (non-secure HTTP LAN dev)
  return {
    ciphertext: btoa(JSON.stringify(payload)),
    nonce: btoa(crypto.randomUUID().slice(0, 12))
  };
}

export async function decryptVaultPayload<T>(
  ciphertextBase64: string,
  nonceBase64: string,
  vaultSecret?: string
): Promise<T | null> {
  if (!ciphertextBase64) return null;

  const key = await getCachedVaultKey(vaultSecret);
  if (key && nonceBase64) {
    try {
      return await decryptVaultItem<T>(ciphertextBase64, nonceBase64, key);
    } catch {
      // Fallback to default secret or legacy Base64 JSON below
    }
    if (vaultSecret && vaultSecret !== FALLBACK_SECRET_KEY) {
      try {
        const defaultKey = await getCachedVaultKey(FALLBACK_SECRET_KEY);
        if (defaultKey) return await decryptVaultItem<T>(ciphertextBase64, nonceBase64, defaultKey);
      } catch {
        // Fallback to legacy Base64 JSON below
      }
    }
  }

  // Backward compatibility for legacy unencrypted Base64 items
  try {
    return JSON.parse(atob(ciphertextBase64)) as T;
  } catch {
    return null;
  }
}

function bufferToBase64(buffer: Uint8Array): string {
  let binary = '';
  for (let i = 0; i < buffer.byteLength; i++) {
    binary += String.fromCharCode(buffer[i]);
  }
  return btoa(binary);
}

function base64ToBuffer(base64: string): Uint8Array {
  const binary = atob(base64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}
