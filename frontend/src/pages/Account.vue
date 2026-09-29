<script setup lang="ts">
import { useRouter } from "vue-router";
import { api, state, notify } from "../api";
import AddressBook from "../components/AddressBook.vue";
import Icon from "../components/Icon.vue";
const router = useRouter();
async function logout() {
  try {
    await api("/auth/logout", "POST", {});
    state.session = null;
    state.cart = [];
    notify("已安全退出");
    router.push("/");
  } catch (e) {
    notify((e as Error).message, true);
  }
}
</script>
<template>
  <div class="page-container">
    <div class="page-heading">
      <div class="eyebrow">YOUR PERSONAL SPACE</div>
      <h1>我的脉冲生活</h1>
      <p>好物、期待和生活，都安排得刚刚好。</p>
    </div>
    <div v-if="!state.session" class="empty-state">
      <Icon name="user" :size="44" />
      <h2>这里，期待你的到来</h2>
      <RouterLink class="btn dark" to="/login">登录 / 注册</RouterLink>
    </div>
    <div v-else class="account-layout">
      <aside class="profile-card">
        <div class="profile-avatar">
          {{ state.session.user.name.slice(0, 1) }}
        </div>
        <h2>{{ state.session.user.name }}</h2>
        <p>{{ state.session.user.email }}</p>
        <span class="pill">{{
          state.session.user.role === "admin" ? "商城管理员" : "脉冲生活会员"
        }}</span>
        <div class="profile-links">
          <RouterLink to="/orders"
            ><Icon name="box" />我的订单<Icon
              name="chevron"
              :size="16" /></RouterLink
          ><RouterLink to="/cart"
            ><Icon name="bag" />我的购物袋<Icon
              name="chevron"
              :size="16" /></RouterLink
          ><RouterLink v-if="state.session.user.role === 'admin'" to="/admin"
            ><Icon name="chart" />运营工作台<Icon
              name="chevron"
              :size="16" /></RouterLink
          ><button @click="logout"><Icon name="logout" />安全退出</button>
        </div>
      </aside>
      <div>
        <AddressBook />
        <section class="account-note">
          <Icon name="shield" :size="28" />
          <div>
            <h3>你的订单，只对你可见</h3>
            <p>
              在这里管理收货地址、查看订单进度。使用公共设备后，请及时退出账户。
            </p>
            <small>当前支付与物流为模拟服务，不产生真实扣款。</small>
          </div>
        </section>
      </div>
    </div>
  </div>
</template>
