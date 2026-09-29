<script setup lang="ts">
import { ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { api, state, money, notify, refreshCart } from "../api";
import type { Product } from "../types";
import ProductArt from "../components/ProductArt.vue";
import Icon from "../components/Icon.vue";
const route = useRoute(),
  router = useRouter(),
  product = ref<Product | null>(null),
  quantity = ref(1),
  busy = ref(false),
  error = ref("");
async function load() {
  error.value = "";
  product.value = null;
  try {
    product.value = await api<Product>("/products/" + route.params.id);
    quantity.value = 1;
  } catch (e) {
    error.value = (e as Error).message;
  }
}
watch(() => route.params.id, load, { immediate: true });
async function add(checkout = false) {
  if (!state.session) {
    router.push("/login");
    return;
  }
  if (!product.value) return;
  busy.value = true;
  try {
    const current =
      state.cart.find((i) => i.product.id === product.value!.id)?.quantity || 0;
    await api("/cart/" + product.value.id, "PUT", {
      quantity: current + quantity.value,
    });
    await refreshCart();
    if (checkout) router.push("/cart");
    else notify("已加入购物袋");
  } catch (e) {
    notify((e as Error).message, true);
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <div class="page-container">
    <div class="breadcrumb">
      <RouterLink to="/">首页</RouterLink><span>/</span
      ><RouterLink to="/shop">探索好物</RouterLink><span>/</span
      ><span>{{ product?.name || "商品详情" }}</span>
    </div>
    <div v-if="error" class="error-state">
      {{ error }}<RouterLink class="btn dark" to="/shop">返回商城</RouterLink>
    </div>
    <div v-else-if="!product" class="loading-page">
      <span class="spinner"></span>
    </div>
    <template v-else
      ><div class="product-detail">
        <div class="detail-art">
          <ProductArt :type="product.image" large /><span
            class="detail-art-label"
            >PULSE / EVERYDAY ESSENTIALS</span
          >
        </div>
        <div class="detail-copy">
          <span class="pill">{{ product.category }}</span>
          <h1>{{ product.name }}</h1>
          <p class="detail-subtitle">{{ product.subtitle }}</p>
          <div class="detail-price">
            <strong>¥{{ money(product.price_cents) }}</strong
            ><del>¥{{ money(product.original_price_cents) }}</del
            ><span>全场包邮</span>
          </div>
          <div class="detail-divider"></div>
          <div class="detail-property">
            <span>商品系列</span><b>{{ product.category }} · 日常精选</b>
          </div>
          <div class="detail-property">
            <span>现货库存</span
            ><b>{{ product.stock > 0 ? product.stock + " 件" : "暂时售罄" }}</b>
          </div>
          <div class="detail-property">
            <span>购买数量</span>
            <div class="quantity-control">
              <button
                :disabled="quantity <= 1"
                @click="quantity--"
                aria-label="减少数量"
              >
                −</button
              ><span>{{ quantity }}</span
              ><button
                :disabled="quantity >= Math.min(99, product.stock)"
                @click="quantity++"
                aria-label="增加数量"
              >
                ＋
              </button>
            </div>
          </div>
          <div class="detail-buttons">
            <button
              class="btn dark"
              :disabled="busy || product.stock < 1"
              @click="add(true)"
            >
              立即选购<Icon name="arrow" :size="18" /></button
            ><button
              class="btn outline"
              :disabled="busy || product.stock < 1"
              @click="add(false)"
            >
              <Icon name="bag" :size="18" />加入购物袋
            </button>
          </div>
          <p class="secure-note">
            <Icon name="shield" :size="16" />安全会话 · 订单权限保护 ·
            全程模拟交易
          </p>
        </div>
      </div>
      <div class="product-story">
        <div>
          <div class="eyebrow">DESIGNED FOR DAILY LIFE</div>
          <h2>好设计，是日常的陪伴。</h2>
        </div>
        <p>{{ product.description }}</p>
      </div></template
    >
  </div>
</template>
