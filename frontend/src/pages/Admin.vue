<script setup lang="ts">
import { confirmAction, promptAction } from "../dialog";
import { computed, onMounted, reactive, ref, watch } from "vue";
import { api, state, money, date, notify, statusText } from "../api";
import type { Product, Order, Activity, Page } from "../types";
import Icon from "../components/Icon.vue";
import ProductArt from "../components/ProductArt.vue";
const tab = ref("overview"),
  loading = ref(false),
  busy = ref(false),
  error = ref(""),
  stats = ref<Record<string, number>>({}),
  products = ref<Product[]>([]),
  orders = ref<Page<Order>>({ items: [], total: 0, page: 1, size: 20 }),
  activities = ref<Activity[]>([]),
  audit = ref<
    Array<{
      id: number;
      actor_id: number;
      action: string;
      target: string;
      created_at: number;
    }>
  >([]),
  ops = ref<{
    outbox: Array<{
      id: string;
      kind: string;
      status: string;
      attempts: number;
      last_error: string;
    }>;
    invariants: Array<Record<string, number>>;
  }>({ outbox: [], invariants: [] }),
  orderStatus = ref(""),
  productPage = ref(1),
  productTotal = ref(0);
const tabs = [
  { id: "overview", name: "运营总览", icon: "chart" },
  { id: "products", name: "商品管理", icon: "grid" },
  { id: "activities", name: "秒杀活动", icon: "bolt" },
  { id: "orders", name: "订单履约", icon: "box" },
  { id: "ops", name: "秒杀运行监控", icon: "shield" },
  { id: "audit", name: "操作审计", icon: "clock" },
];
const activeTitle = computed(() => tabs.find((t) => t.id === tab.value)?.name);
const editing = ref(false),
  draft = reactive({
    id: 0,
    name: "",
    subtitle: "",
    description: "",
    category: "桌面数码",
    image: "keyboard",
    price: 199,
    original: 299,
    stock: 100,
    status: "active",
    featured: false,
  });
function localTime(ms: number) {
  const d = new Date(ms);
  return new Date(ms - d.getTimezoneOffset() * 60000)
    .toISOString()
    .slice(0, 16);
}
const creatingActivity = ref(false),
  campaign = reactive({
    product_id: 0,
    price: 99,
    stock: 100,
    start: localTime(Date.now() + 60000),
    end: localTime(Date.now() + 6 * 3600000),
  });
async function load() {
  if (state.session?.user.role !== "admin") return;
  loading.value = true;
  error.value = "";
  try {
    if (tab.value === "overview" || tab.value === "ops")
      stats.value = await api<Record<string, number>>("/admin/stats");
    if (tab.value === "products" || tab.value === "activities") {
      const p = await api<Page<Product>>(
        "/admin/products?size=50&page=" + productPage.value,
      );
      products.value = p.items;
      productTotal.value = p.total;
    }
    if (tab.value === "overview" || tab.value === "orders")
      orders.value = await api<Page<Order>>(
        "/admin/orders?size=20&page=" +
          orders.value.page +
          "&status=" +
          orderStatus.value,
      );
    if (tab.value === "activities")
      activities.value = await api<Activity[]>("/admin/activities");
    if (tab.value === "ops") ops.value = await api("/admin/ops");
    if (tab.value === "audit") audit.value = await api("/admin/audit");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}
watch(tab, () => {
  orders.value.page = 1;
  load();
});
watch(orderStatus, () => {
  orders.value.page = 1;
  load();
});
onMounted(load);
function editProduct(p?: Product) {
  Object.assign(
    draft,
    p
      ? {
          id: p.id,
          name: p.name,
          subtitle: p.subtitle,
          description: p.description,
          category: p.category,
          image: p.image,
          price: p.price_cents / 100,
          original: p.original_price_cents / 100,
          stock: p.stock,
          status: p.status,
          featured: p.featured,
        }
      : {
          id: 0,
          name: "",
          subtitle: "",
          description: "",
          category: "桌面数码",
          image: "keyboard",
          price: 199,
          original: 299,
          stock: 100,
          status: "active",
          featured: false,
        },
  );
  editing.value = true;
}
async function saveProduct() {
  busy.value = true;
  try {
    await api(
      draft.id ? "/admin/products/" + draft.id : "/admin/products",
      draft.id ? "PUT" : "POST",
      {
        id: draft.id,
        name: draft.name,
        subtitle: draft.subtitle,
        description: draft.description,
        category: draft.category,
        image: draft.image,
        price_cents: Math.round(draft.price * 100),
        original_price_cents: Math.round(draft.original * 100),
        stock: draft.stock,
        status: draft.status,
        featured: draft.featured,
      },
    );
    editing.value = false;
    notify("商品已保存");
    await load();
  } catch (e) {
    notify((e as Error).message, true);
  } finally {
    busy.value = false;
  }
}
async function restock(p: Product) {
  const text = await promptAction(
    "为“" + p.name + "”补充多少件普通库存？",
    "100",
  );
  if (text === null) return;
  const quantity = Number(text);
  if (!Number.isInteger(quantity) || quantity <= 0) {
    notify("请输入正整数", true);
    return;
  }
  try {
    await api("/admin/products/" + p.id + "/restock", "POST", { quantity });
    notify("补货成功");
    await load();
  } catch (e) {
    notify((e as Error).message, true);
  }
}
async function createActivity() {
  busy.value = true;
  try {
    await api("/admin/activities", "POST", {
      product_id: campaign.product_id,
      price_cents: Math.round(campaign.price * 100),
      stock: campaign.stock,
      starts_at: new Date(campaign.start).getTime(),
      ends_at: new Date(campaign.end).getTime(),
    });
    creatingActivity.value = false;
    notify("活动已创建，普通库存已划拨。点击“预热并发布”开始准入。");
    await load();
  } catch (e) {
    notify((e as Error).message, true);
  } finally {
    busy.value = false;
  }
}
async function manage(a: Activity, action: string) {
  if (
    action === "rebuild" &&
    !(await confirmAction(
      "重建会暂停准入并关闭尚未完成的排队请求，然后按数据库库存恢复。继续？",
    ))
  )
    return;
  busy.value = true;
  try {
    await api("/admin/activities/" + a.id + "/" + action, "POST", {});
    notify(
      action === "pause" ? "活动准入已暂停" : "活动库存已同步，准入已开启",
    );
    await load();
  } catch (e) {
    notify((e as Error).message, true);
  } finally {
    busy.value = false;
  }
}
async function ship(o: Order) {
  const tracking = await promptAction(
    "输入模拟物流信息，例如：顺丰 SF202600001",
  );
  if (tracking === null) return;
  try {
    await api("/admin/orders/" + o.id + "/ship", "POST", { tracking });
    notify("已标记为发货");
    await load();
  } catch (e) {
    notify((e as Error).message, true);
  }
}
async function retryDead() {
  busy.value = true;
  try {
    const out = await api<{ requeued: number }>(
      "/admin/ops/retry-dead",
      "POST",
      {},
    );
    notify("已重新投递 " + out.requeued + " 条消息");
    await load();
  } catch (e) {
    notify((e as Error).message, true);
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <div class="page-container admin-page">
    <div v-if="state.session?.user.role !== 'admin'" class="empty-state">
      <Icon name="shield" :size="44" />
      <h1>这里是运营工作台</h1>
      <p>请使用管理员账户登录后访问。</p>
      <RouterLink class="btn dark" to="/login">前往登录</RouterLink>
    </div>
    <div v-else class="admin-layout">
      <aside class="admin-sidebar">
        <div class="eyebrow">PULSE CONSOLE</div>
        <h2>运营工作台</h2>
        <nav>
          <button
            v-for="t in tabs"
            :key="t.id"
            :class="{ active: tab === t.id }"
            @click="tab = t.id"
          >
            <Icon :name="t.icon" :size="18" />{{ t.name }}
          </button>
        </nav>
        <div class="admin-sidebar-note">
          <Icon name="shield" /><b>权限已验证</b
          ><small
            >{{ state.session.user.name }}<br />全部管理操作记录审计日志</small
          >
        </div>
      </aside>
      <section class="admin-content">
        <div class="admin-heading">
          <div>
            <small>工作台 / {{ activeTitle }}</small>
            <h1>{{ activeTitle }}</h1>
          </div>
          <button class="btn outline small" @click="load" :disabled="loading">
            <Icon name="refresh" :size="16" />刷新数据
          </button>
        </div>
        <div v-if="error" class="form-error">{{ error }}</div>
        <template v-if="tab === 'overview'"
          ><div class="stat-grid">
            <div class="stat-card">
              <span>实付金额 · 模拟</span
              ><strong>¥{{ money(stats.revenue_cents || 0) }}</strong
              ><small>已支付 / 已发货 / 已完成订单</small>
            </div>
            <div class="stat-card">
              <span>累计订单</span
              ><strong>{{ stats.orders || 0 }}<i>笔</i></strong
              ><small>秒杀订单 {{ stats.flash_orders || 0 }} 笔</small>
            </div>
            <div class="stat-card">
              <span>注册用户</span
              ><strong>{{ stats.users || 0 }}<i>人</i></strong
              ><small>普通商城账户</small>
            </div>
            <div class="stat-card">
              <span>在库商品</span
              ><strong>{{ stats.products || 0 }}<i>款</i></strong
              ><small>待支付订单 {{ stats.pending_orders || 0 }} 笔</small>
            </div>
          </div>
          <div class="admin-feature-banner">
            <div>
              <span class="eyebrow">THE HEART OF PULSE</span>
              <h2>高并发秒杀，是商城的心跳。</h2>
              <p>原子准入 → 持久化请求 → 可靠投递 → 幂等下单 → 超时返库</p>
            </div>
            <button class="btn dark" @click="tab = 'ops'">
              查看运行状态<Icon name="arrow" :size="17" />
            </button>
          </div>
          <div class="subsection-heading">
            <h3>最近订单</h3>
            <button class="text-link" @click="tab = 'orders'">
              查看全部 →
            </button>
          </div>
          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>订单</th>
                  <th>类型</th>
                  <th>状态</th>
                  <th>金额</th>
                  <th>创建时间</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="o in orders.items.slice(0, 8)" :key="o.id">
                  <td class="mono">{{ o.id.slice(-12) }}</td>
                  <td>{{ o.kind === "flash" ? "秒杀" : "普通" }}</td>
                  <td>
                    <span class="status-badge" :class="o.status">{{
                      statusText[o.status]
                    }}</span>
                  </td>
                  <td>¥{{ money(o.total_cents) }}</td>
                  <td>{{ date(o.created_at) }}</td>
                </tr>
                <tr v-if="!orders.items.length">
                  <td colspan="5" class="table-empty">
                    还没有订单，去前台体验一次完整流程。
                  </td>
                </tr>
              </tbody>
            </table>
          </div></template
        >
        <template v-if="tab === 'products'"
          ><div class="subsection-heading">
            <p>共 {{ productTotal }} 款商品 · 编辑信息不会覆盖实时库存</p>
            <button class="btn dark small" @click="editProduct()">
              <Icon name="plus" :size="16" />新增商品
            </button>
          </div>
          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>商品</th>
                  <th>分类</th>
                  <th>售价</th>
                  <th>普通库存</th>
                  <th>状态</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="p in products" :key="p.id">
                  <td>
                    <div class="table-product">
                      <ProductArt :type="p.image" />
                      <div>
                        <b>{{ p.name }}</b
                        ><small>#{{ p.id }}</small>
                      </div>
                    </div>
                  </td>
                  <td>{{ p.category }}</td>
                  <td>¥{{ money(p.price_cents) }}</td>
                  <td>{{ p.stock }}</td>
                  <td>
                    <span
                      class="pill"
                      :class="{ muted: p.status !== 'active' }"
                      >{{ p.status === "active" ? "已上架" : "已下架" }}</span
                    >
                  </td>
                  <td>
                    <div class="table-actions">
                      <button @click="editProduct(p)">编辑</button
                      ><button @click="restock(p)">补货</button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <div v-if="productTotal > 50" class="pagination">
            <button
              :disabled="productPage <= 1"
              @click="
                productPage--;
                load();
              "
            >
              上一页</button
            ><span>{{ productPage }}</span
            ><button
              :disabled="productPage * 50 >= productTotal"
              @click="
                productPage++;
                load();
              "
            >
              下一页
            </button>
          </div></template
        >
        <template v-if="tab === 'activities'"
          ><div class="subsection-heading">
            <p>每场活动拥有独立库存配额，结束后回收未使用库存。</p>
            <button class="btn dark small" @click="creatingActivity = true">
              <Icon name="plus" :size="16" />创建活动
            </button>
          </div>
          <div class="admin-activity-grid">
            <article v-for="a in activities" :key="a.id" class="admin-activity">
              <div class="admin-activity-title">
                <ProductArt :type="a.image" />
                <div>
                  <span
                    class="pill"
                    :class="{ orange: a.status === 'active' }"
                    >{{
                      a.status === "active"
                        ? "准入开启"
                        : a.status === "paused"
                          ? "已暂停"
                          : "已归档"
                    }}</span
                  >
                  <h3>{{ a.name }}</h3>
                  <small
                    >场次 #{{ a.id }} · 秒杀价 ¥{{
                      money(a.price_cents)
                    }}</small
                  >
                </div>
              </div>
              <div class="activity-numbers">
                <div>
                  <span>初始配额</span><b>{{ a.initial_stock }}</b>
                </div>
                <div>
                  <span>数据库余量</span><b>{{ a.stock }}</b>
                </div>
                <div>
                  <span>可抢名额</span><b>{{ a.available }}</b>
                </div>
              </div>
              <p>{{ date(a.starts_at) }} → {{ date(a.ends_at) }}</p>
              <small
                v-if="a.status === 'active' && !a.ready"
                class="danger-text"
                >缓存准入尚未就绪，需要检查并重建。</small
              >
              <div v-if="a.status !== 'archived'" class="button-row">
                <button
                  v-if="a.status === 'paused'"
                  class="btn dark small"
                  :disabled="busy"
                  @click="manage(a, 'publish')"
                >
                  预热并发布</button
                ><button
                  v-if="a.status === 'active'"
                  class="btn outline small"
                  :disabled="busy"
                  @click="manage(a, 'pause')"
                >
                  暂停准入</button
                ><button
                  class="btn outline small"
                  :disabled="busy"
                  @click="manage(a, 'rebuild')"
                >
                  重建库存
                </button>
              </div>
            </article>
          </div></template
        >
        <template v-if="tab === 'orders'"
          ><div class="orders-toolbar">
            <select v-model="orderStatus" aria-label="筛选订单状态">
              <option value="">所有状态</option>
              <option
                v-for="s in [
                  'pending',
                  'paid',
                  'shipped',
                  'completed',
                  'cancelled',
                  'expired',
                  'refunded',
                ]"
                :key="s"
                :value="s"
              >
                {{ statusText[s] }}
              </option></select
            ><small>共 {{ orders.total }} 笔订单</small>
          </div>
          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>订单 / 商品</th>
                  <th>收件人</th>
                  <th>金额</th>
                  <th>状态</th>
                  <th>履约操作</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="o in orders.items" :key="o.id">
                  <td>
                    <small class="mono">{{ o.id.slice(-12) }}</small>
                    <div v-for="i in o.items" :key="i.product_id">
                      {{ i.name }} × {{ i.quantity }}
                    </div>
                    <span v-if="o.kind === 'flash'" class="pill orange"
                      >秒杀</span
                    >
                  </td>
                  <td>
                    <b>{{ o.address.recipient }}</b
                    ><small
                      >{{ o.address.phone }}<br />{{ o.address.region }}
                      {{ o.address.detail }}</small
                    >
                  </td>
                  <td>¥{{ money(o.total_cents) }}</td>
                  <td>
                    <span class="status-badge" :class="o.status">{{
                      statusText[o.status]
                    }}</span>
                  </td>
                  <td>
                    <button
                      v-if="o.status === 'paid'"
                      class="btn dark small"
                      @click="ship(o)"
                    >
                      模拟发货</button
                    ><small v-else>{{ o.tracking || "—" }}</small>
                  </td>
                </tr>
                <tr v-if="!orders.items.length">
                  <td colspan="5" class="table-empty">没有符合条件的订单。</td>
                </tr>
              </tbody>
            </table>
          </div>
          <div v-if="orders.total > orders.size" class="pagination">
            <button
              :disabled="orders.page <= 1"
              @click="
                orders.page--;
                load();
              "
            >
              上一页</button
            ><span
              >{{ orders.page }} /
              {{ Math.ceil(orders.total / orders.size) }}</span
            ><button
              :disabled="orders.page * orders.size >= orders.total"
              @click="
                orders.page++;
                load();
              "
            >
              下一页
            </button>
          </div></template
        >
        <template v-if="tab === 'ops'"
          ><div class="stat-grid">
            <div class="stat-card">
              <span>排队中的请求</span
              ><strong>{{ stats.queued_tickets || 0 }}</strong
              ><small>超过两分钟未完成将自动关闭</small>
            </div>
            <div class="stat-card">
              <span>待投递事件</span
              ><strong>{{ stats.outbox_pending || 0 }}</strong
              ><small>数据库 Outbox 自动重试</small>
            </div>
            <div class="stat-card">
              <span>死信消息</span
              ><strong>{{
                stats.dead_letters === -1 ? "不可用" : stats.dead_letters || 0
              }}</strong
              ><small>检查原因后再投递</small>
            </div>
            <div class="stat-card">
              <span>已生成秒杀订单</span
              ><strong>{{ stats.flash_orders || 0 }}</strong
              ><small>数据库持久记录</small>
            </div>
          </div>
          <div class="pipeline-view">
            <span>Redis 原子准入</span><i>→</i><span>MySQL + Outbox</span
            ><i>→</i><span>RabbitMQ</span><i>→</i><span>幂等消费</span><i>→</i
            ><span>订单 / 返库</span>
          </div>
          <p v-if="state.demo" class="form-notice">
            当前为本地预览环境，商品与订单为示例数据。
          </p>
          <div class="subsection-heading">
            <h3>库存守恒检查</h3>
            <small
              >差额 = 初始配额 − 剩余库存 − 有效订单 − 已返还普通库存</small
            >
          </div>
          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>场次</th>
                  <th>初始配额</th>
                  <th>剩余库存</th>
                  <th>有效订单</th>
                  <th>已返还</th>
                  <th>差额</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="c in ops.invariants" :key="c.activity_id">
                  <td>#{{ c.activity_id }}</td>
                  <td>{{ c.initial_stock }}</td>
                  <td>{{ c.remaining_stock }}</td>
                  <td>{{ c.active_orders }}</td>
                  <td>{{ c.returned }}</td>
                  <td>
                    <span
                      :class="c.difference === 0 ? 'green-text' : 'danger-text'"
                      >{{ c.difference === 0 ? "✓ 守恒" : c.difference }}</span
                    >
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <div class="subsection-heading">
            <h3>待投递与重试事件</h3>
            <button
              class="btn outline small"
              :disabled="busy || !stats.dead_letters || stats.dead_letters < 0"
              @click="retryDead"
            >
              重新投递死信
            </button>
          </div>
          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>事件</th>
                  <th>类型</th>
                  <th>状态</th>
                  <th>尝试次数</th>
                  <th>最近错误</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="e in ops.outbox" :key="e.id">
                  <td class="mono">{{ e.id.slice(0, 22) }}…</td>
                  <td>{{ e.kind }}</td>
                  <td>{{ e.status }}</td>
                  <td>{{ e.attempts }}</td>
                  <td>{{ e.last_error || "—" }}</td>
                </tr>
                <tr v-if="!ops.outbox.length">
                  <td colspan="5" class="table-empty">
                    全部事件已投递，当前没有积压。
                  </td>
                </tr>
              </tbody>
            </table>
          </div></template
        >
        <template v-if="tab === 'audit'"
          ><p class="admin-caption">
            最近 100 条操作，包含商品、活动、订单状态变更和消息重投递。
          </p>
          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>时间</th>
                  <th>操作者</th>
                  <th>动作</th>
                  <th>目标</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="e in audit" :key="e.id">
                  <td>{{ date(e.created_at) }}</td>
                  <td>{{ e.actor_id ? "用户 #" + e.actor_id : "系统" }}</td>
                  <td>{{ e.action }}</td>
                  <td class="mono">{{ e.target }}</td>
                </tr>
              </tbody>
            </table>
          </div></template
        >
      </section>
    </div>
    <div v-if="editing" class="modal-overlay" @click.self="editing = false">
      <section
        class="modal-card"
        role="dialog"
        aria-modal="true"
        aria-labelledby="product-form-title"
      >
        <div class="subsection-heading">
          <h2 id="product-form-title">
            {{ draft.id ? "编辑商品" : "新增商品" }}
          </h2>
          <button
            class="icon-button"
            @click="editing = false"
            aria-label="关闭"
          >
            <Icon name="close" />
          </button>
        </div>
        <form @submit.prevent="saveProduct">
          <label
            >商品名称<input
              v-model="draft.name"
              minlength="2"
              maxlength="100"
              required /></label
          ><label
            >一句话介绍<input v-model="draft.subtitle" maxlength="120"
          /></label>
          <div class="form-grid">
            <label
              >分类<select v-model="draft.category">
                <option>桌面数码</option>
                <option>通勤随行</option>
                <option>品质生活</option>
              </select></label
            ><label
              >商品插画<select v-model="draft.image">
                <option
                  v-for="i in [
                    'keyboard',
                    'headphones',
                    'lamp',
                    'speaker',
                    'bag',
                    'bottle',
                    'watch',
                    'camera',
                    'mouse',
                  ]"
                  :key="i"
                >
                  {{ i }}
                </option>
              </select></label
            ><label
              >售价（元）<input
                v-model.number="draft.price"
                type="number"
                min="0.01"
                step="0.01"
                required /></label
            ><label
              >划线价（元）<input
                v-model.number="draft.original"
                type="number"
                :min="draft.price"
                step="0.01"
                required /></label
            ><label v-if="!draft.id"
              >初始库存<input
                v-model.number="draft.stock"
                type="number"
                min="0"
                max="1000000"
                required /></label
            ><label
              >上架状态<select v-model="draft.status">
                <option value="active">上架</option>
                <option value="hidden">下架</option>
              </select></label
            >
          </div>
          <label
            >商品介绍<textarea
              v-model="draft.description"
              maxlength="3000"
              rows="4"
            ></textarea></label
          ><label class="checkbox-label"
            ><input
              v-model="draft.featured"
              type="checkbox"
            />设为编辑精选</label
          ><button class="btn dark full" :disabled="busy">保存商品</button>
        </form>
      </section>
    </div>
    <div
      v-if="creatingActivity"
      class="modal-overlay"
      @click.self="creatingActivity = false"
    >
      <section
        class="modal-card"
        role="dialog"
        aria-modal="true"
        aria-labelledby="activity-form-title"
      >
        <div class="subsection-heading">
          <h2 id="activity-form-title">创建秒杀活动</h2>
          <button
            class="icon-button"
            @click="creatingActivity = false"
            aria-label="关闭"
          >
            <Icon name="close" />
          </button>
        </div>
        <p class="form-notice">
          配额将从普通商品库存中划拨。创建后，点击“预热并发布”才会开放抢购。
        </p>
        <form @submit.prevent="createActivity">
          <label
            >活动商品<select v-model.number="campaign.product_id" required>
              <option :value="0" disabled>请选择商品</option>
              <option
                v-for="p in products.filter((x) => x.status === 'active')"
                :key="p.id"
                :value="p.id"
              >
                {{ p.name }} · 库存 {{ p.stock }}
              </option>
            </select></label
          >
          <div class="form-grid">
            <label
              >秒杀价（元）<input
                v-model.number="campaign.price"
                type="number"
                min="0.01"
                step="0.01"
                required /></label
            ><label
              >独立库存配额<input
                v-model.number="campaign.stock"
                type="number"
                min="1"
                max="100000"
                required /></label
            ><label
              >开始时间<input
                v-model="campaign.start"
                type="datetime-local"
                required /></label
            ><label
              >结束时间<input
                v-model="campaign.end"
                type="datetime-local"
                required
            /></label>
          </div>
          <button class="btn dark full" :disabled="busy">创建活动</button>
        </form>
      </section>
    </div>
  </div>
</template>
