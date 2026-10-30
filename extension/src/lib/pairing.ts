/**
 * Pairing codes look like XXXXX-XXXXX (10 characters, 50 bits). Accept any
 * case, missing or extra dashes and spaces; send the canonical form.
 */
export function normalizePairingCode(input: string): string | null {
  const chars = input.toUpperCase().replace(/[\s-]+/g, '');
  if (!/^[A-Z0-9]{10}$/.test(chars)) return null;
  return `${chars.slice(0, 5)}-${chars.slice(5)}`;
}

interface UAData {
  brands?: { brand: string; version: string }[];
  platform?: string;
}

/** A readable default device name such as "Chrome on Linux". */
export function defaultDeviceName(nav: { userAgent: string; userAgentData?: UAData } = navigator as never): string {
  const ua = nav.userAgent ?? '';
  const brands = nav.userAgentData?.brands?.map((b) => b.brand) ?? [];
  const pick = (names: string[]) => names.find((n) => brands.includes(n));
  let browserName =
    pick(['Microsoft Edge', 'Brave', 'Opera', 'CocCoc', 'Google Chrome']) ??
    (/Edg\//.test(ua) ? 'Microsoft Edge' : /coc_coc|CocCoc/i.test(ua) ? 'Cốc Cốc' : /OPR\//.test(ua) ? 'Opera' : /Chrome\//.test(ua) ? 'Chrome' : 'Browser');
  if (browserName === 'Google Chrome') browserName = 'Chrome';
  if (browserName === 'CocCoc') browserName = 'Cốc Cốc';
  let os = nav.userAgentData?.platform || '';
  if (!os) os = /Windows/.test(ua) ? 'Windows' : /Mac OS X/.test(ua) ? 'macOS' : /CrOS/.test(ua) ? 'ChromeOS' : /Android/.test(ua) ? 'Android' : /Linux/.test(ua) ? 'Linux' : '';
  return os ? `${browserName} on ${os}` : browserName;
}
