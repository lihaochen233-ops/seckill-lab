<script setup lang="ts">
import { confirmAction, promptAction } from "../dialog";
import { onMounted, onUnmounted, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { api, state, money, date, statusText, notify } from "../api";
import type { Order, Page } from "../types";
import ProductArt from "../components/ProductArt.vue";
import Icon from "../components/Icon.vue";
const route = useRoute(),
  data = ref<Page<Order>>({ items: [], total: 0, page: 1, size: 10 }),
  status = ref(""),
  expanded = ref(String(route.query.id || "")),
  busy = ref(""),
  loading = ref(false),
  error = ref(""),
  now = ref(Date.now());
let timer: ReturnType<typeof setInterval>;
async function load(page = 1) {
  if (!state.session) return;
  loading.value = true;
  error.value = "";
  try {
    data.value = await api<Page<Order>>(
      "/orders?size=10&page=" + page + "&status=" + status.value,
    );
    if (
      route.query.id &&
      !data.value.items.some((o) => o.id === route.query.id)
    ) {
      const o = await api<Order>("/orders/" + route.query.id);
      data.value.items.unshift(o);
    }
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}
function remaining(o: Order) {
  const s = Math.max(0, Math.ceil((o.expires_at - now.value) / 1000));
  return s
    ? Math.floor(s / 60) + "分" + (s % 60) + "秒"
    : "已到期，等待系统关单";
}
async function action(o: Order, kind: string) {
  const message: Record<string, string> = {
    pay: "确认模拟支付 ¥" + money(o.total_cents) + "？不会产生真实扣款。",
    cancel: "确认取消此订单并释放库存？",
    refund: "确认模拟退款并释放库存？秒杀参与资格不会恢复。",
    receive: "确认已经收到商品？",
  };
  if (!(await confirmAction(message[kind]))) return;
  busy.value = o.id;
  try {
    await api<Order>("/orders/" + o.id + "/" + kind, "POST", {});
    notify(
      {
        pay: "模拟支付成功",
        cancel: "订单已取消",
        refund: "模拟退款成功",
        receive: "感谢确认收货",
      }[kind] || "操作成功",
    );
    await load(data.value.page);
  } catch (e) {
    notify((e as Error).message, true);
  } finally {
    busy.value = "";
  }
}
watch(status, () => load());
onMounted(() => {
  load();
  timer = setInterval(() => (now.value = Date.now()), 1000);
});
onUnmounted(() => clearInterval(timer));
</script>
<template>
  <div class="page-container">
    <div class="page-heading">
      <div class="eyebrow">EVERY ORDER, A LITTLE EXPECTATION</div>
      <h1>我的订单</h1>
      <p>从心动到收货，每一步都在这里。</p>
    </div>
    <div v-if="!state.session" class="empty-state">
      <Icon name="box" :size="44" />
      <h2>登录后查看你的订单</h2>
      <RouterLink class="btn dark" to="/login">前往登录</RouterLink>
    </div>
    <template v-else
      ><div class="orders-toolbar">
        <div class="filter-chips">
          <button
            v-for="s in [
              '',
              'pending',
              'paid',
              'shipped',
              'completed',
              'cancelled',
              'refunded',
            ]"
            :key="s"
            :class="{ selected: status === s }"
            @click="status = s"
          >
            {{ s ? statusText[s] : "全部订单" }}
          </button>
        </div>
        <button
          class="icon-button"
          @click="load(data.page)"
          aria-label="刷新订单"
        >
          <Icon name="refresh" />
        </button>
      </div>
      <div v-if="error" class="error-state">
        {{ error }}<button class="btn outline" @click="load()">重新加载</button>
      </div>
      <div v-else-if="loading" class="loading-inline">
        <span class="spinner"></span>
      </div>
      <div v-else-if="!data.items.length" class="empty-state">
        <Icon name="box" :size="44" />
        <h2>还没有这类订单</h2>
        <p>每一份期待，都从发现好物开始。</p>
        <RouterLink class="btn dark" to="/shop">去逛逛 →</RouterLink>
      </div>
      <article v-for="o in data.items" :key="o.id" class="order-card">
        <header>
          <div>
            <span class="pill" :class="{ orange: o.kind === 'flash' }">{{
              o.kind === "flash" ? "秒杀订单" : "日常好物"
            }}</span
            ><small>{{ date(o.created_at) }} · {{ o.id.slice(-12) }}</small>
          </div>
          <span class="status-badge" :class="o.status">{{
            statusText[o.status]
          }}</span>
        </header>
        <div v-for="item in o.items" :key="item.product_id" class="order-item">
          <ProductArt :type="item.image" />
          <div>
            <h3>{{ item.name }}</h3>
            <small>¥{{ money(item.price_cents) }} × {{ item.quantity }}</small>
          </div>
          <b>¥{{ money(item.price_cents * item.quantity) }}</b>
        </div>
        <div class="order-bottom">
          <div>
            <small v-if="o.status === 'pending'" class="orange-text"
              ><Icon name="clock" :size="14" />剩余支付时间：{{
                remaining(o)
              }}</small
            ><button
              class="text-link"
              @click="expanded = expanded === o.id ? '' : o.id"
            >
              {{ expanded === o.id ? "收起详情" : "订单详情" }} ↓
            </button>
          </div>
          <div class="order-actions">
            <span
              >合计 <strong>¥{{ money(o.total_cents) }}</strong></span
            ><button
              v-if="o.status === 'pending'"
              class="btn outline small"
              :disabled="busy === o.id"
              @click="action(o, 'cancel')"
            >
              取消订单</button
            ><button
              v-if="o.status === 'pending'"
              class="btn dark small"
              :disabled="busy === o.id || now >= o.expires_at"
              @click="action(o, 'pay')"
            >
              模拟支付</button
            ><button
              v-if="o.status === 'paid'"
              class="btn outline small"
              :disabled="busy === o.id"
              @click="action(o, 'refund')"
            >
              模拟退款</button
            ><button
              v-if="o.status === 'shipped'"
              class="btn dark small"
              :disabled="busy === o.id"
              @click="action(o, 'receive')"
            >
              确认收货
            </button>
          </div>
        </div>
        <div v-if="expanded === o.id" class="order-details">
          <p>
            <b>收货信息</b>{{ o.address.recipient }} · {{ o.address.phone
            }}<br />{{ o.address.region }} {{ o.address.detail }}
          </p>
          <p>
            <b>订单编号</b><span class="mono">{{ o.id }}</span>
          </p>
          <p v-if="o.tracking"><b>物流信息</b>{{ o.tracking }}</p>
          <p v-if="o.paid_at">
            <b>支付时间</b>{{ date(o.paid_at) }}（模拟支付）
          </p>
          <small
            >订单保存下单时的商品价格和地址快照，后续编辑不会改变本订单。</small
          >
        </div>
      </article>
      <div v-if="data.total > data.size" class="pagination">
        <button :disabled="data.page <= 1" @click="load(data.page - 1)">
          上一页</button
        ><span>{{ data.page }} / {{ Math.ceil(data.total / data.size) }}</span
        ><button
          :disabled="data.page * data.size >= data.total"
          @click="load(data.page + 1)"
        >
          下一页
        </button>
      </div></template
    >
  </div>
</template>
