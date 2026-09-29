<script setup lang="ts">
import ActionDialog from "./components/ActionDialog.vue";
import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { initialize, state } from "./api";
import Icon from "./components/Icon.vue";
const route = useRoute(),
  router = useRouter(),
  search = ref(""),
  mobile = ref(false);
const count = computed(() => state.cart.reduce((n, i) => n + i.quantity, 0));
onMounted(initialize);
function submitSearch() {
  router.push({ path: "/shop", query: { q: search.value } });
  mobile.value = false;
}
</script>
<template>
  <ActionDialog />
  <div class="app-shell">
    <div class="announcement">
      <span>让好设计，走进每一天</span
      ><span class="announcement-right"
        >全场包邮 · 限时库存保留 · 模拟交易体验</span
      >
    </div>
    <header class="site-header">
      <div class="header-inner">
        <RouterLink class="brand" to="/" aria-label="PULSE 首页"
          ><span class="brand-symbol">p<span>·</span></span>
          <div><b>PULSE</b><small>脉冲生活</small></div></RouterLink
        >
        <nav class="desktop-nav">
          <RouterLink to="/" :class="{ active: route.path === '/' }"
            >首页</RouterLink
          ><RouterLink to="/shop" :class="{ active: route.path === '/shop' }"
            >探索好物</RouterLink
          ><RouterLink to="/flash" :class="{ active: route.path === '/flash' }"
            ><Icon name="bolt" :size="15" />限时秒杀<span class="nav-dot"></span
          ></RouterLink>
        </nav>
        <form class="header-search" @submit.prevent="submitSearch">
          <Icon name="search" :size="17" /><input
            v-model="search"
            placeholder="发现你的下一件心头好"
            aria-label="搜索商品"
          /><button aria-label="搜索" type="submit">↵</button>
        </form>
        <div class="header-actions">
          <RouterLink
            to="/orders"
            class="icon-button orders-shortcut"
            aria-label="我的订单"
            ><Icon name="box" /></RouterLink
          ><RouterLink
            :to="state.session ? '/account' : '/login'"
            class="icon-button"
            aria-label="个人中心"
            ><Icon name="user" /></RouterLink
          ><RouterLink
            to="/cart"
            class="icon-button cart-button"
            aria-label="购物袋"
            ><Icon name="bag" /><span v-if="count" class="cart-count">{{
              count
            }}</span></RouterLink
          ><button
            class="icon-button mobile-toggle"
            @click="mobile = !mobile"
            aria-label="展开导航"
          >
            <Icon name="menu" />
          </button>
        </div>
      </div>
      <nav v-if="mobile" class="mobile-nav" @click="mobile = false">
        <RouterLink to="/">首页</RouterLink
        ><RouterLink to="/shop">探索好物</RouterLink
        ><RouterLink to="/flash">限时秒杀</RouterLink
        ><RouterLink to="/orders">我的订单</RouterLink>
      </nav>
    </header>
    <main>
      <RouterView v-if="state.ready" />
      <div v-else class="loading-page">
        <span class="spinner"></span>
        <p>正在打开你的生活灵感…</p>
      </div>
    </main>
    <section class="service-strip">
      <div>
        <Icon name="box" /><span
          ><b>全场包邮</b><small>好物轻松送到家</small></span
        >
      </div>
      <div>
        <Icon name="shield" /><span
          ><b>账户保护</b><small>安心管理你的订单</small></span
        >
      </div>
      <div>
        <Icon name="refresh" /><span
          ><b>未发货可退款</b><small>模拟支付，无真实扣款</small></span
        >
      </div>
      <div>
        <Icon name="bolt" /><span
          ><b>公平限购</b><small>每场秒杀每人一次</small></span
        >
      </div>
    </section>
    <footer>
      <div class="footer-main">
        <RouterLink class="brand" to="/"
          ><span class="brand-symbol">p<span>·</span></span>
          <div><b>PULSE</b><small>让日常，轻快一点。</small></div></RouterLink
        >
        <div class="footer-links">
          <RouterLink to="/shop">探索好物</RouterLink
          ><RouterLink to="/flash">秒杀专区</RouterLink
          ><RouterLink to="/orders">订单服务</RouterLink
          ><RouterLink v-if="state.session?.user.role === 'admin'" to="/admin"
            >运营后台</RouterLink
          >
        </div>
      </div>
      <div class="footer-bottom">
        <span>© 2026 PULSE 脉冲生活</span
        ><span
          >{{
            state.demo
              ? "本地预览 · 示例商品"
              : "PULSE 在线商城"
          }}
          · 所有支付均为模拟</span
        >
      </div>
    </footer>
    <Transition name="toast"
      ><div
        v-if="state.toast"
        class="toast-message"
        :class="{ error: state.toastError }"
        role="status"
      >
        <Icon :name="state.toastError ? 'close' : 'check'" />{{ state.toast
        }}<button @click="state.toast = ''" aria-label="关闭提示">×</button>
      </div></Transition
    >
  </div>
</template>
