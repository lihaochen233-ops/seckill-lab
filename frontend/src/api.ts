import { reactive } from "vue";
import type { Session, CartItem } from "./types";
export const state = reactive({
  session: null as Session | null,
  cart: [] as CartItem[],
  ready: false,
  demo: false,
  toast: "",
  toastError: false,
  loading: 0,
});
let toastTimer: ReturnType<typeof setTimeout> | undefined;
export function notify(message: string, error = false) {
  state.toast = message;
  state.toastError = error;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => (state.toast = ""), 5000);
}
export class APIError extends Error {
  constructor(
    message: string,
    public status: number,
    public code: string,
  ) {
    super(message);
  }
}
// 所有页面共用同一请求入口：写操作带 CSRF，结算与秒杀提交额外带幂等键。
export async function api<T>(
  path: string,
  method = "GET",
  data?: unknown,
  key?: string,
): Promise<T> {
  const headers: Record<string, string> = {};
  if (method !== "GET") {
    headers["Content-Type"] = "application/json";
    if (state.session) headers["X-CSRF-Token"] = state.session.csrf_token;
  }
  if (key) headers["Idempotency-Key"] = key;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 15000);
  try {
    const res = await fetch("/api" + path, {
      method,
      headers,
      credentials: "same-origin",
      body: data === undefined ? undefined : JSON.stringify(data),
      signal: controller.signal,
    });
    const out = await res.json();
    if (!res.ok) {
      if (res.status === 401) {
        state.session = null;
        state.cart = [];
      }
      throw new APIError(
        out.message || "请求失败",
        res.status,
        out.code || "unknown",
      );
    }
    return out as T;
  } finally {
    clearTimeout(timer);
  }
}
export async function refreshCart() {
  if (!state.session) {
    state.cart = [];
    return;
  }
  state.cart = await api<CartItem[]>("/cart");
}
export async function initialize() {
  try {
    const cfg = await api<{ demo: boolean }>("/config");
    state.demo = cfg.demo;
    try {
      state.session = await api<Session>("/auth/session");
      await refreshCart();
    } catch (err) {
      if (!(err instanceof APIError && err.status === 401))
        notify("登录状态暂时无法读取，请稍后重试", true);
    }
  } catch {
    notify("无法连接商城服务，请检查 Go 后端是否启动", true);
  } finally {
    state.ready = true;
  }
}
export function money(cents: number) {
  // 后端始终以“分”为单位存储金额，页面只在展示时转换成元。
  return (cents / 100).toFixed(2);
}
export function date(ms: number) {
  return new Date(ms).toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}
export const statusText: Record<string, string> = {
  pending: "待支付",
  paid: "待发货",
  shipped: "待收货",
  completed: "已完成",
  cancelled: "已取消",
  expired: "已超时",
  refunded: "已退款",
  queued: "排队处理中",
  ordered: "已生成订单",
  rejected: "未抢到名额",
};
export function newKey() {
  return crypto.randomUUID();
}
// 请求编号不包含登录凭证；页面刷新后仍能安全重试同一笔结算。
export function persistentKey(scope: string) {
  const key = "pulse.request." + scope;
  let value = sessionStorage.getItem(key);
  if (!value) {
    value = newKey();
    sessionStorage.setItem(key, value);
  }
  return value;
}
export function clearKey(scope: string) {
  sessionStorage.removeItem("pulse.request." + scope);
}
