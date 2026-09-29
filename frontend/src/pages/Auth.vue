<script setup lang="ts">
import { reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { api, state, notify, refreshCart } from "../api";
import type { Session } from "../types";
import ProductArt from "../components/ProductArt.vue";
import Icon from "../components/Icon.vue";
const router = useRouter(),
  register = ref(false),
  busy = ref(false),
  error = ref(""),
  form = reactive({ email: "", password: "", name: "" });
async function submit() {
  busy.value = true;
  error.value = "";
  try {
    const payload = register.value
      ? form
      : { email: form.email, password: form.password };
    state.session = await api<Session>(
      register.value ? "/auth/register" : "/auth/login",
      "POST",
      payload,
    );
    await refreshCart();
    notify("欢迎，" + state.session.user.name);
    router.push(state.session.user.role === "admin" ? "/admin" : "/account");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
function fill(admin = false) {
  register.value = false;
  form.email = admin ? "admin@pulse.local" : "demo@pulse.local";
  form.password = admin ? "PulseAdmin2026!" : "PulseDemo2026!";
  error.value = "";
}
</script>
<template>
  <div class="page-container auth-layout">
    <section class="auth-editorial">
      <div class="eyebrow">WELCOME TO YOUR EVERYDAY</div>
      <h1>好生活，<br />从一件心动好物开始。</h1>
      <p>登录后收藏生活灵感，参与限时秒杀，<br />让每一份期待都有迹可循。</p>
      <ProductArt type="headphones" large /><span
        >PULSE / MADE FOR THE WAY YOU LIVE</span
      >
    </section>
    <section class="auth-card">
      <span class="pill">{{
        register ? "加入脉冲生活" : "很高兴再次见到你"
      }}</span>
      <h2>{{ register ? "创建你的账户" : "欢迎回来" }}</h2>
      <p>
        {{ register ? "开启你的好物探索之旅" : "登录，继续发现日常的小美好" }}
      </p>
      <form @submit.prevent="submit">
        <label v-if="register"
          >你的昵称<input
            v-model="form.name"
            minlength="2"
            maxlength="30"
            required
            autocomplete="nickname"
            placeholder="怎么称呼你？" /></label
        ><label
          >邮箱地址<input
            v-model="form.email"
            type="email"
            maxlength="160"
            required
            autocomplete="email"
            placeholder="you@example.com" /></label
        ><label
          >密码<input
            v-model="form.password"
            type="password"
            :minlength="register ? 10 : 1"
            maxlength="128"
            required
            :autocomplete="register ? 'new-password' : 'current-password'"
            placeholder="至少 10 个字符"
        /></label>
        <div v-if="error" class="form-error" role="alert">{{ error }}</div>
        <button class="btn dark full" :disabled="busy">
          {{ busy ? "请稍候…" : register ? "注册并登录" : "登录账户"
          }}<Icon name="arrow" :size="18" />
        </button>
      </form>
      <p class="auth-switch">
        {{ register ? "已有账户？" : "还没有账户？"
        }}<button
          @click="
            register = !register;
            error = '';
          "
        >
          {{ register ? "立即登录" : "免费注册" }}
        </button>
      </p>
      <div v-if="state.demo" class="demo-accounts">
        <b>本地演示账户</b>
        <p>无需注册，填入演示账号后点击登录。</p>
        <div>
          <button @click="fill(false)">体验用户</button
          ><button @click="fill(true)">管理员</button>
        </div>
        <small>仅演示环境提供，完整部署请创建自己的账号。</small>
      </div>
      <p class="secure-note">
        <Icon name="shield" :size="15" />请妥善保管账户信息，勿向他人透露密码。
      </p>
    </section>
  </div>
</template>
