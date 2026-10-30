import { fileURLToPath } from 'node:url';

export const SELLER_PORT = 4710;
export const API_PORT = 4711;
export const SELLER_ORIGIN = `http://localhost:${SELLER_PORT}`;
export const API_ORIGIN = `http://localhost:${API_PORT}`;
export const KEY_ID = 'e2e-key';
export const TMP_DIR = fileURLToPath(new URL('./.tmp/', import.meta.url));
export const KEY_FILE = `${TMP_DIR}signing-key.pem`;
export const EXTENSION_DIR = fileURLToPath(new URL('../.output/chrome-mv3-test/', import.meta.url));
export const PAIR_CODE = 'ABCDE-FGHJK';
