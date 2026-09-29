<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import {
  api,
  state,
  money,
  notify,
  refreshCart,
  persistentKey,
  clearKey,
} from "../api";
import type { Order } from "../types";
import ProductArt from "../components/ProductArt.vue";
import Icon from "../components/Icon.vue";
import AddressBook from "../components/AddressBook.vue";
const router = useRouter(),
  address = ref(0),
  busy = ref(false),
  error = ref("");
const total = computed(() =>
  state.cart.reduce((n, i) => n + i.product.price_cents * i.quantity, 0),
);
const count = computed(() => state.cart.reduce((n, i) => n + i.quantity, 0));
async function quantity(id: number, q: number) {
  busy.value = true;
  try {
    await api("/cart/" + id, "PUT", { quantity: q });
    await refreshCart();
  } catch (e) {
    notify((e as Error).message, true);
  } finally {
    busy.value = false;
  }
}
async function checkout() {
  if (!address.value) {
    notify("请先选择收货地址", true);
    return;
  }
  busy.value = true;
  error.value = "";
  const items = state.cart.map((i) => ({
    product_id: i.product.id,
    quantity: i.quantity,
  }));
  // 同一份结算内容复用请求编号；网络超时后重试不会再创建一笔新订单。
  const scope =
    "checkout:" +
    state.session?.user.id +
    ":" +
    address.value +
    ":" +
    JSON.stringify(items);
  try {
    const o = await api<Order>(
      "/orders",
      "POST",
      { address_id: address.value, items },
      persistentKey(scope),
    );
    clearKey(scope);
    await refreshCart();
    notify("订单已创建，请在倒计时内完成模拟支付");
    router.push("/orders?id=" + o.id);
  } catch (e) {
    error.value =
      (e as Error).message +
      "。如网络中断，请保持当前页面重试，或先到“我的订单”确认结果。";
  } finally {
    busy.value = false;
  }
}
onMounted(async () => {
  if (state.session) {
    try {
      await refreshCart();
    } catch (e) {
      error.value = (e as Error).message;
    }
  }
});
</script>
<template>
  <div class="page-container">
    <div class="page-heading">
      <div class="eyebrow">YOUR LITTLE COLLECTION</div>
      <h1>
        购物袋<span class="heading-count">{{ count }}</span>
      </h1>
      <p>把心动好物，带进日常。</p>
    </div>
    <div v-if="!state.session" class="empty-state">
      <Icon name="bag" :size="42" />
      <h2>登录后，装下你的心动</h2>
      <RouterLink class="btn dark" to="/login">前往登录</RouterLink>
    </div>
    <div v-else-if="!state.cart.length" class="empty-state">
      <Icon name="bag" :size="42" />
      <h2>购物袋里，期待第一件好物</h2>
      <p>去探索那些实用又有趣的日常灵感。</p>
      <RouterLink class="btn dark" to="/shop">探索好物 →</RouterLink>
    </div>
    <div v-else class="checkout-layout">
      <section>
        <div class="cart-list">
          <article
            v-for="item in state.cart"
            :key="item.product.id"
            class="cart-item"
          >
            <RouterLink :to="'/products/' + item.product.id"
              ><ProductArt :type="item.product.image"
            /></RouterLink>
            <div class="cart-item-copy">
              <small>{{ item.product.category }}</small
              ><RouterLink :to="'/products/' + item.product.id"
                ><h3>{{ item.product.name }}</h3></RouterLink
              >
              <p>{{ item.product.subtitle }}</p>
              <strong>¥{{ money(item.product.price_cents) }}</strong
              ><span
                v-if="
                  item.product.status !== 'active' ||
                  item.product.stock < item.quantity
                "
                class="danger-text"
                >商品已下架或库存不足，请调整购物袋</span
              >
            </div>
            <div class="cart-item-side">
              <button
                class="icon-button"
                :disabled="busy"
                @click="quantity(item.product.id, 0)"
                :aria-label="'移除 ' + item.product.name"
              >
                <Icon name="trash" :size="17" />
              </button>
              <div class="quantity-control">
                <button
                  :disabled="busy || item.quantity <= 1"
                  @click="quantity(item.product.id, item.quantity - 1)"
                  aria-label="减少数量"
                >
                  −</button
                ><span>{{ item.quantity }}</span
                ><button
                  :disabled="
                    busy || item.quantity >= Math.min(99, item.product.stock)
                  "
                  @click="quantity(item.product.id, item.quantity + 1)"
                  aria-label="增加数量"
                >
                  ＋
                </button>
              </div>
              <b>¥{{ money(item.product.price_cents * item.quantity) }}</b>
            </div>
          </article>
        </div>
        <AddressBook selectable @select="address = $event" />
      </section>
      <aside class="checkout-summary">
        <h2>订单小计</h2>
        <div>
          <span>商品金额（{{ count }} 件）</span><b>¥{{ money(total) }}</b>
        </div>
        <div><span>配送费用</span><span class="green-text">免运费</span></div>
        <div class="summary-total">
          <span>合计</span><strong>¥{{ money(total) }}</strong>
        </div>
        <div v-if="error" class="form-error">
          {{ error }}<RouterLink to="/orders">查看我的订单</RouterLink>
        </div>
        <button
          class="btn dark full"
          :disabled="busy || !address"
          @click="checkout"
        >
          {{ busy ? "正在创建订单…" : "确认下单"
          }}<Icon name="arrow" :size="18" />
        </button>
        <p>
          <Icon name="clock" :size="15" />下单后为你保留库存，超时自动释放。
        </p>
        <p><Icon name="shield" :size="15" />此为模拟交易，不产生真实扣款。</p>
      </aside>
    </div>
  </div>
</template>
