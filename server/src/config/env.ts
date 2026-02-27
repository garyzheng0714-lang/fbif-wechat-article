import dotenv from 'dotenv';
import { z } from 'zod';
import { fileURLToPath } from 'url';
import { dirname, resolve } from 'path';

const __dirname = dirname(fileURLToPath(import.meta.url));
dotenv.config({ path: resolve(__dirname, '../../../.env') });

const envSchema = z.object({
  NODE_ENV: z.enum(['development', 'test', 'production']).default('development'),
  SERVER_PORT: z.coerce.number().default(3001),
  CLIENT_ORIGIN: z.string().default('http://localhost:5173'),
  WECHAT_APPID: z.string().default(''),
  WECHAT_SECRET: z.string().default(''),
  FEISHU_APP_ID: z.string().default(''),
  FEISHU_APP_SECRET: z.string().default(''),
  FEISHU_BITABLE_APP_TOKEN: z.string().default(''),
  FEISHU_BITABLE_TABLE_ID: z.string().default(''),
  FEISHU_BITABLE_TABLE_ID_USERS: z.string().default(''),
});

export const env = envSchema.parse(process.env);

if (!env.WECHAT_APPID || !env.WECHAT_SECRET) {
  console.warn('[Warning] WECHAT_APPID or WECHAT_SECRET not set. API calls will fail.');
  console.warn('[Warning] Copy .env.example to .env and fill in your credentials.');
}
