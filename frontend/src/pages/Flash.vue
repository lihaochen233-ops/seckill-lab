<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { useRouter } from "vue-router";
import { api, state, money, notify, persistentKey, statusText } from "../api";
import type { Activity, Ticket } from "../types";
import ProductArt from "../components/ProductArt.vue";
import Icon from "../components/Icon.vue";
import AddressBook from "../components/AddressBook.vue";
const router = useRouter(),
  activities = ref<Activity[]>([]),
  tickets = ref<Ticket[]>([]),
  address = ref(0),
  busy = ref(0),
  now = ref(Date.now()),
  error = ref("");
let clock: ReturnType<typeof setInterval>,
  poll: ReturnType<typeof setInterval>,
  refresh: ReturnType<typeof setInterval>;
let alive = true,
  fetching = false;
const currentEnd = computed(
  () =>
    activities.value.find(
      (a) => a.starts_at <= now.value && a.ends_at > now.value,
    )?.ends_at ||
    activities.value.find((a) => a.starts_at > now.value)?.starts_at ||
    0,
);
function countdown(target: number) {
  const s = Math.max(0, Math.floor((target - now.value) / 1000));
  return [Math.floor(s / 3600), Math.floor(s / 60) % 60, s % 60].map((n) =>
    String(n).padStart(2, "0"),
  );
}
function label(a: Activity) {
  if (a.ends_at <= now.value) return "本场已结束";
  if (a.starts_at > now.value) return "即将开抢";
  if (!a.ready) return "活动准备中";
  if (a.available <= 0) return "名额已抢完";
  return "立即抢购";
}
function result(a: Activity) {
  return tickets.value.find((t) => t.activity_id === a.id);
}
async function load() {
  try {
    const out = await api<Activity[]>("/activities");
    if (alive) activities.value = out;
    error.value = "";
  } catch (e) {
    if (alive) error.value = (e as Error).message;
  }
}
async function loadTickets() {
  if (!state.session || fetching) return;
  fetching = true;
  try {
    const out = await api<Ticket[]>("/tickets");
    if (alive) tickets.value = out;
  } catch {
  } finally {
    fetching = false;
  }
}
async function join(a: Activity) {
  if (!state.session) {
    router.push("/login");
    return;
  }
  if (!address.value) {
    notify("请先选择下方的收货地址", true);
    return;
  }
  busy.value = a.id;
  try {
    // 每个用户在每场活动复用同一编号；排队票据的最终结果由后续轮询确认。
    const t = await api<Ticket>(
      "/activities/" + a.id + "/join",
      "POST",
      { address_id: address.value },
      persistentKey("flash:" + state.session.user.id + ":" + a.id),
    );
    tickets.value = [t, ...tickets.value.filter((x) => x.id !== t.id)];
    notify(
      t.status === "queued"
        ? "请求已受理，正在确认抢购结果"
        : t.status === "ordered"
          ? "抢购成功，请前往订单支付"
          : t.reason,
      t.status === "rejected",
    );
    await load();
  } catch (e) {
    notify((e as Error).message, true);
    await loadTickets();
  } finally {
    busy.value = 0;
  }
}
onMounted(async () => {
  await Promise.all([load(), loadTickets()]);
  if (!alive) return;
  clock = setInterval(() => (now.value = Date.now()), 1000);
  // 只有存在 queued 票据才轮询结果，避免空闲页面持续请求订单状态。
  poll = setInterval(() => {
    if (tickets.value.some((t) => t.status === "queued")) loadTickets();
  }, 1500);
  refresh = setInterval(load, 10000);
});
onUnmounted(() => {
  alive = false;
  clearInterval(clock);
  clearInterval(poll);
  clearInterval(refresh);
});
</script>
<template>
  <div class="page-container">
    <section class="flash-hero">
      <div>
        <div class="eyebrow orange">
          <Icon name="bolt" :size="15" />PULSE FLASH / 限时秒杀
        </div>
        <h1>好物不等人，<br />心动<span>就现在。</span></h1>
        <p>
          限量配额，限时好价。每人每场一次机会，<br />抢到后及时支付，让期待成为日常。
        </p>
        <div class="flash-rules">
          <span><Icon name="shield" :size="15" />公平限购</span
          ><span><Icon name="clock" :size="15" />超时自动释放</span
          ><span><Icon name="box" :size="15" />全场包邮</span>
        </div>
      </div>
      <div class="flash-hero-right">
        <div class="bolt-orbit"><Icon name="bolt" :size="72" /></div>
        <span>{{ currentEnd ? "当前场次倒计时" : "新活动即将上线" }}</span>
        <div class="countdown">
          <template v-for="(part, i) in countdown(currentEnd)" :key="i"
            ><b>{{ part }}</b
            ><em v-if="i < 2">:</em></template
          >
        </div>
        <small>实际名额与活动时间以服务器确认为准</small>
      </div>
    </section>
    <div class="section-heading">
      <div>
        <h2>正在发生的心动</h2>
        <p>点击后先受理请求，再确认结果；请勿频繁重复刷新。</p>
      </div>
      <button
        class="btn outline small"
        @click="
          load();
          loadTickets();
        "
      >
        <Icon name="refresh" :size="16" />刷新场次
      </button>
    </div>
    <div v-if="error" class="error-state">
      {{ error }}<button class="btn outline" @click="load">重试</button>
    </div>
    <div class="flash-grid">
      <article v-for="a in activities" :key="a.id" class="flash-card">
        <div class="flash-art">
          <ProductArt :type="a.image" /><span class="pill orange">{{
            a.starts_at > now ? "即将开始" : "限时好价"
          }}</span>
        </div>
        <div class="flash-card-copy">
          <small>场次 #{{ a.id }} · 每人限购 1 件</small>
          <h3>{{ a.name }}</h3>
          <div class="flash-card-price">
            <strong>¥{{ money(a.price_cents) }}</strong
            ><del>¥{{ money(a.original_price_cents) }}</del
            ><span
              >省 ¥{{ money(a.original_price_cents - a.price_cents) }}</span
            >
          </div>
          <div class="stock-progress">
            <i
              :style="{
                width:
                  Math.max(
                    0,
                    Math.min(
                      100,
                      ((a.initial_stock - a.available) / a.initial_stock) * 100,
                    ),
                  ) + '%',
              }"
            ></i>
          </div>
          <div class="stock-caption">
            <span
              >剩余可抢名额 {{ Math.max(0, a.available) }} /
              {{ a.initial_stock }}</span
            ><span
              >{{ a.starts_at > now ? "距开始" : "距结束" }}
              {{
                countdown(a.starts_at > now ? a.starts_at : a.ends_at).join(":")
              }}</span
            >
          </div>
          <template v-if="result(a)"
            ><div class="ticket-result" :class="result(a)?.status">
              <span
                v-if="result(a)?.status === 'queued'"
                class="spinner small-spinner"
              ></span
              ><Icon
                v-else
                :name="result(a)?.status === 'ordered' ? 'check' : 'clock'"
                :size="18"
              />
              <div>
                <b>{{ statusText[result(a)!.status] }}</b
                ><small>{{
                  result(a)?.status === "queued"
                    ? "正在排队确认，结果会自动更新"
                    : result(a)?.status === "ordered"
                      ? "订单已生成，请及时完成模拟支付"
                      : result(a)?.reason
                }}</small>
              </div>
            </div>
            <RouterLink
              v-if="result(a)?.order_id"
              :to="'/orders?id=' + result(a)?.order_id"
              class="btn dark full"
              >查看订单 / 去支付<Icon
                name="arrow"
                :size="17" /></RouterLink></template
          ><button
            v-else
            class="btn flash-btn full"
            :disabled="
              !!busy ||
              a.starts_at > now ||
              a.ends_at <= now ||
              !a.ready ||
              a.available <= 0
            "
            @click="join(a)"
          >
            {{ busy === a.id ? "正在提交…" : label(a)
            }}<Icon name="bolt" :size="18" />
          </button>
        </div>
      </article>
    </div>
    <div v-if="!activities.length && !error" class="empty-state">
      <h2>下一场惊喜，正在准备</h2>
      <RouterLink class="btn outline" to="/shop">先去逛逛好物</RouterLink>
    </div>
    <div class="flash-bottom">
      <AddressBook v-if="state.session" selectable @select="address = $event" />
      <div v-else class="panel">
        <h3>提前登录，开抢更从容</h3>
        <p>登录后添加收货地址，即可参与秒杀。</p>
        <RouterLink class="btn dark" to="/login">登录 / 注册</RouterLink>
      </div>
      <aside class="flash-explainer">
        <h3>抢购小贴士</h3>
        <ol>
          <li><b>选好收货地址</b><span>开抢前准备好，减少操作步骤。</span></li>
          <li>
            <b>受理后等待结果</b><span>显示“排队处理中”时，尚未生成订单。</span>
          </li>
          <li>
            <b>抢到后及时支付</b
            ><span>超时、取消或未发货退款会返还名额；本场参与资格不恢复。</span>
          </li>
        </ol>
      </aside>
    </div>
  </div>
</template>
