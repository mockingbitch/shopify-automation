import { describe, expect, it } from 'vitest';
import { defaultDeviceName, normalizePairingCode } from '@/lib/pairing';

describe('pairing code', () => {
  it('normalizes case and dashes', () => {
    expect(normalizePairingCode('abcde-fghjk')).toBe('ABCDE-FGHJK');
    expect(normalizePairingCode('ABCDEFGHJK')).toBe('ABCDE-FGHJK');
    expect(normalizePairingCode(' ab cde - fg hjk ')).toBe('ABCDE-FGHJK');
  });
  it('rejects wrong lengths and characters', () => {
    expect(normalizePairingCode('ABCDE-FGHJ')).toBeNull();
    expect(normalizePairingCode('ABCDE-FGHJKL')).toBeNull();
    expect(normalizePairingCode('ABCDE_FGHJK')).toBeNull();
  });
  it('derives a device name', () => {
    expect(defaultDeviceName({ userAgent: 'Mozilla/5.0 (X11; Linux x86_64) Chrome/156.0 Safari/537.36' })).toBe('Chrome on Linux');
    expect(defaultDeviceName({ userAgent: 'x', userAgentData: { brands: [{ brand: 'Microsoft Edge', version: '1' }], platform: 'Windows' } })).toBe('Microsoft Edge on Windows');
  });
});
