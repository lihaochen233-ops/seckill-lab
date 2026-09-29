<script setup lang="ts">
import { ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { api } from "../api";
import type { Product, Page } from "../types";
import ProductCard from "../components/ProductCard.vue";
import Icon from "../components/Icon.vue";
const route = useRoute(),
  router = useRouter(),
  data = ref<Page<Product>>({ items: [], total: 0, page: 1, size: 12 }),
  loading = ref(false),
  error = ref(""),
  query = ref("");
let serial = 0;
function update(values: Record<string, string>) {
  router.push({
    path: "/shop",
    query: { ...route.query, page: "1", ...values },
  });
}
async function load() {
  const turn = ++serial;
  loading.value = true;
  error.value = "";
  query.value = String(route.query.q || "");
  try {
    const params = new URLSearchParams();
    for (const key of ["q", "category", "sort", "page"]) {
      if (route.query[key]) params.set(key, String(route.query[key]));
    }
    const out = await api<Page<Product>>("/products?" + params);
    if (turn === serial) data.value = out;
  } catch (e) {
    if (turn === serial) error.value = (e as Error).message;
  } finally {
    if (turn === serial) loading.value = false;
  }
}
watch(() => route.fullPath, load, { immediate: true });
</script>
<template>
  <div class="page-container">
    <div class="page-heading">
      <div class="eyebrow">THE EVERYDAY COLLECTION</div>
      <h1>探索好物</h1>
      <p>有用，也有趣。找到属于你的生活灵感。</p>
    </div>
    <div class="catalog-toolbar">
      <div class="filter-chips">
        <button
          v-for="c in ['全部', '桌面数码', '通勤随行', '品质生活']"
          :key="c"
          :class="{
            selected:
              (!route.query.category && c === '全部') ||
              route.query.category === c,
          }"
          @click="update({ category: c === '全部' ? '' : c })"
        >
          {{ c }}
        </button>
      </div>
      <form class="inline-search" @submit.prevent="update({ q: query })">
        <Icon name="search" :size="17" /><input
          v-model="query"
          aria-label="搜索好物"
          placeholder="搜索好物名称"
        /><button type="submit">搜索</button>
      </form>
      <select
        :value="route.query.sort || ''"
        @change="update({ sort: ($event.target as HTMLSelectElement).value })"
        aria-label="商品排序"
      >
        <option value="">精选推荐</option>
        <option value="newest">最新上架</option>
        <option value="price_asc">价格从低到高</option>
        <option value="price_desc">价格从高到低</option>
      </select>
    </div>
    <div class="result-meta">
      共 {{ data.total }} 件好物<span v-if="route.query.q">
        · “{{ route.query.q }}” 的搜索结果
        <button @click="update({ q: '' })">清除搜索</button></span
      >
    </div>
    <div v-if="error" class="error-state">
      {{ error }}<button class="btn outline" @click="load">重试</button>
    </div>
    <div v-else-if="loading" class="product-grid">
      <div v-for="n in 8" :key="n" class="skeleton-card"></div>
    </div>
    <div v-else-if="data.items.length" class="product-grid">
      <ProductCard v-for="p in data.items" :key="p.id" :product="p" />
    </div>
    <div v-else class="empty-state">
      <Icon name="search" :size="40" />
      <h2>还没有找到这件好物</h2>
      <p>试试其他关键词，或看看全部商品。</p>
      <button class="btn dark" @click="router.push('/shop')">查看全部</button>
    </div>
    <div v-if="data.total > data.size" class="pagination">
      <button
        :disabled="data.page <= 1"
        @click="update({ page: String(data.page - 1) })"
      >
        上一页</button
      ><span>{{ data.page }} / {{ Math.ceil(data.total / data.size) }}</span
      ><button
        :disabled="data.page * data.size >= data.total"
        @click="update({ page: String(data.page + 1) })"
      >
        下一页
      </button>
    </div>
  </div>
</template>
