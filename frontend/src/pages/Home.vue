<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { api, money } from "../api";
import type { Product, Activity, Page } from "../types";
import ProductArt from "../components/ProductArt.vue";
import ProductCard from "../components/ProductCard.vue";
import Icon from "../components/Icon.vue";
const products = ref<Product[]>([]),
  activities = ref<Activity[]>([]),
  category = ref("全部"),
  error = ref(""),
  loading = ref(true);
const selected = computed(() =>
  products.value
    .filter((p) => category.value === "全部" || p.category === category.value)
    .slice(0, 6),
);
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const [ps, as] = await Promise.all([
      api<Page<Product>>("/products?size=20"),
      api<Activity[]>("/activities"),
    ]);
    products.value = ps.items;
    activities.value = as.filter((a) => a.ends_at > Date.now()).slice(0, 3);
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}
onMounted(load);
</script>
<template>
  <div class="page-container home-page">
    <section class="hero">
      <div class="hero-copy">
        <div class="eyebrow"><span></span> GOOD DESIGN, EVERYDAY</div>
        <h1>生活值得，<br />一点<span>好设计。</span></h1>
        <p>
          从一张书桌，到每一次出发。<br />精选实用与美感兼具的好物，让日常轻快一点。
        </p>
        <div class="hero-actions">
          <RouterLink class="btn dark" to="/shop"
            >探索本周精选<Icon name="arrow" :size="18" /></RouterLink
          ><RouterLink class="text-link" to="/flash"
            >限时好价 <span>↗</span></RouterLink
          >
        </div>
        <div class="hero-bottom">
          <div class="little-avatars">
            <span>P</span><span>U</span><span>L</span><span>S</span
            ><span>E</span>
          </div>
          <small>为认真生活的你，挑选每一件好物</small>
        </div>
      </div>
      <div class="hero-visual">
        <div class="hero-orbit"></div>
        <div class="hero-art-main"><ProductArt type="keyboard" large /></div>
        <div class="hero-art-second"><ProductArt type="headphones" /></div>
        <div class="floating-label">
          <span class="label-dot"></span>
          <div>
            <small>本周灵感好物</small><b>AIR 75 无线机械键盘</b
            ><span>简洁桌面，从这里开始</span>
          </div>
          <RouterLink to="/shop?q=AIR" aria-label="查看 AIR 键盘"
            ><Icon name="arrow"
          /></RouterLink>
        </div>
        <span class="visual-caption">LESS, BUT BETTER.</span
        ><span class="hero-number">01 / DAILY ESSENTIALS</span>
      </div>
    </section>
    <div class="category-shortcuts">
      <RouterLink to="/shop?category=桌面数码"
        ><span class="category-icon"><Icon name="grid" /></span>
        <div><b>桌面数码</b><small>专注，也可以很有趣</small></div>
        <Icon name="chevron" :size="16" /></RouterLink
      ><RouterLink to="/shop?category=通勤随行"
        ><span class="category-icon sand"><Icon name="bag" /></span>
        <div><b>通勤随行</b><small>轻装，去更远的地方</small></div>
        <Icon name="chevron" :size="16" /></RouterLink
      ><RouterLink to="/shop?category=品质生活"
        ><span class="category-icon peach"><Icon name="heart" /></span>
        <div><b>品质生活</b><small>把日常过成喜欢的样子</small></div>
        <Icon name="chevron" :size="16"
      /></RouterLink>
    </div>
    <section class="home-flash">
      <div class="section-heading">
        <div>
          <div class="eyebrow orange">LIMITED TIME / 限量好价</div>
          <h2><Icon name="bolt" :size="26" />心动，不必等太久</h2>
        </div>
        <RouterLink to="/flash" class="text-link"
          >进入秒杀专区 <Icon name="arrow" :size="17"
        /></RouterLink>
      </div>
      <div class="flash-preview-grid">
        <RouterLink
          v-for="a in activities"
          :key="a.id"
          to="/flash"
          class="flash-mini"
          ><ProductArt :type="a.image" />
          <div>
            <span class="pill orange">{{
              a.starts_at > Date.now() ? "即将开始" : "限时秒杀"
            }}</span>
            <h3>{{ a.name }}</h3>
            <div class="price">
              <strong>¥{{ money(a.price_cents) }}</strong
              ><del>¥{{ money(a.original_price_cents) }}</del>
            </div>
            <small>独立活动配额 · 每人限购 1 件</small>
          </div>
          <span class="mini-arrow"><Icon name="arrow" :size="17" /></span
        ></RouterLink>
        <div v-if="!loading && !activities.length" class="empty-inline">
          新一轮心动好价正在准备中。
        </div>
      </div>
    </section>
    <section class="curated">
      <div class="section-heading">
        <div>
          <div class="eyebrow">CURATED FOR YOUR EVERYDAY</div>
          <h2>日常好物，认真挑选</h2>
        </div>
        <div class="filter-chips">
          <button
            v-for="cat in ['全部', '桌面数码', '通勤随行', '品质生活']"
            :key="cat"
            :class="{ selected: category === cat }"
            @click="category = cat"
          >
            {{ cat }}
          </button>
        </div>
      </div>
      <div v-if="error" class="error-state">
        {{ error }}<button class="btn outline" @click="load">重新加载</button>
      </div>
      <div v-else class="product-grid">
        <div v-if="loading" v-for="n in 6" :key="n" class="skeleton-card"></div>
        <ProductCard v-for="p in selected" :key="p.id" :product="p" />
      </div>
      <RouterLink class="btn outline browse-all" to="/shop"
        >浏览全部好物<Icon name="arrow" :size="17"
      /></RouterLink>
    </section>
    <section class="editorial-banner">
      <div>
        <div class="eyebrow">A LITTLE CHANGE, A BETTER DAY</div>
        <h2>一平方米，<br />也能装下生活的热爱。</h2>
        <p>给桌面做一次减法，给心情做一次加法。</p>
        <RouterLink to="/shop?category=桌面数码" class="text-link"
          >打造你的理想桌面 <Icon name="arrow" :size="18"
        /></RouterLink>
      </div>
      <div class="editorial-art">
        <ProductArt type="lamp" /><ProductArt type="speaker" />
      </div>
      <span class="editorial-tag">THE DESK EDIT / 2026</span>
    </section>
  </div>
</template>
