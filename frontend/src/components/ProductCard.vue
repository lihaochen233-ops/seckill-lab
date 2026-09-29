<script setup lang="ts">
import { useRouter } from "vue-router";
import type { Product } from "../types";
import { api, state, notify, refreshCart, money } from "../api";
import ProductArt from "./ProductArt.vue";
import Icon from "./Icon.vue";
import { ref } from "vue";
const props = defineProps<{ product: Product }>();
const router = useRouter();
const busy = ref(false);
async function add() {
  if (!state.session) {
    router.push("/login");
    return;
  }
  busy.value = true;
  try {
    const current =
      state.cart.find((i) => i.product.id === props.product.id)?.quantity || 0;
    await api("/cart/" + props.product.id, "PUT", { quantity: current + 1 });
    await refreshCart();
    notify("已加入购物袋");
  } catch (e) {
    notify((e as Error).message, true);
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <article class="product-card">
    <RouterLink
      :to="'/products/' + product.id"
      class="art-link"
      :aria-label="product.name"
      ><ProductArt :type="product.image" /><span
        v-if="product.featured"
        class="product-label"
        >编辑精选</span
      ></RouterLink
    >
    <div class="product-info">
      <small>{{ product.category }}</small
      ><RouterLink :to="'/products/' + product.id"
        ><h3>{{ product.name }}</h3></RouterLink
      >
      <p>{{ product.subtitle }}</p>
      <div class="price-row">
        <div>
          <strong>¥{{ money(product.price_cents) }}</strong
          ><del>¥{{ money(product.original_price_cents) }}</del>
        </div>
        <button
          class="add-circle"
          @click="add"
          :disabled="busy || product.stock < 1"
          :aria-label="'加入购物袋：' + product.name"
        >
          <Icon name="plus" :size="18" />
        </button>
      </div>
    </div>
  </article>
</template>
