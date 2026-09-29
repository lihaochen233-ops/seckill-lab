import { createApp } from "vue";
import { createRouter, createWebHistory } from "vue-router";
import App from "./App.vue";
import Home from "./pages/Home.vue";
import Catalog from "./pages/Catalog.vue";
import ProductPage from "./pages/Product.vue";
import Flash from "./pages/Flash.vue";
import Cart from "./pages/Cart.vue";
import Orders from "./pages/Orders.vue";
import Account from "./pages/Account.vue";
import Auth from "./pages/Auth.vue";
import Admin from "./pages/Admin.vue";
import NotFound from "./pages/NotFound.vue";
import "./style.css";
const router = createRouter({
  history: createWebHistory(),
  scrollBehavior() {
    return { top: 0 };
  },
  routes: [
    { path: "/", component: Home },
    { path: "/shop", component: Catalog },
    { path: "/products/:id", component: ProductPage },
    { path: "/flash", component: Flash },
    { path: "/cart", component: Cart },
    { path: "/orders", component: Orders },
    { path: "/account", component: Account },
    { path: "/login", component: Auth },
    { path: "/admin", component: Admin },
    { path: "/:pathMatch(.*)*", component: NotFound },
  ],
});
router.afterEach((to) => {
  document.title =
    ({
      "/": "让日常，轻快一点",
      "/shop": "探索好物",
      "/flash": "限时秒杀",
      "/cart": "购物袋",
      "/orders": "我的订单",
      "/account": "个人中心",
      "/login": "欢迎回来",
      "/admin": "运营工作台",
    }[to.path] || "商品详情") + " · PULSE 脉冲生活";
});
createApp(App).use(router).mount("#app");
