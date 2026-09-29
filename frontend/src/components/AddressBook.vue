<script setup lang="ts">
import { confirmAction, promptAction } from "../dialog";
import { onMounted, reactive, ref, watch } from "vue";
import type { Address } from "../types";
import { api, notify } from "../api";
import Icon from "./Icon.vue";
const props = defineProps<{ selectable?: boolean }>(),
  emit = defineEmits<{ select: [id: number] }>(),
  addresses = ref<Address[]>([]),
  selected = ref(0),
  editing = ref(false),
  busy = ref(false),
  error = ref(""),
  form = reactive({ id: 0, recipient: "", phone: "", region: "", detail: "" });
watch(selected, (id) => emit("select", id));
async function load() {
  try {
    addresses.value = await api<Address[]>("/addresses");
    if (!addresses.value.some((a) => a.id === selected.value))
      selected.value = addresses.value[0]?.id || 0;
  } catch (e) {
    error.value = (e as Error).message;
  }
}
function edit(a?: Address) {
  Object.assign(
    form,
    a || { id: 0, recipient: "", phone: "", region: "", detail: "" },
  );
  editing.value = true;
  error.value = "";
}
async function save() {
  busy.value = true;
  error.value = "";
  try {
    const a = await api<Address>(
      form.id ? "/addresses/" + form.id : "/addresses",
      form.id ? "PUT" : "POST",
      form,
    );
    editing.value = false;
    await load();
    selected.value = a.id;
    notify("地址已保存");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
async function remove(a: Address) {
  if (!(await confirmAction("删除这个收货地址？已创建的订单地址不会改变。")))
    return;
  try {
    await api("/addresses/" + a.id, "DELETE", {});
    await load();
    notify("地址已删除");
  } catch (e) {
    notify((e as Error).message, true);
  }
}
onMounted(load);
</script>
<template>
  <section class="address-book">
    <div class="subsection-heading">
      <h3>
        <Icon name="location" :size="18" />{{
          props.selectable ? "选择收货地址" : "收货地址"
        }}
      </h3>
      <button class="text-link" @click="edit()">
        <Icon name="plus" :size="16" />新增地址
      </button>
    </div>
    <div v-if="error" class="form-error">{{ error }}</div>
    <div class="address-grid">
      <article
        v-for="a in addresses"
        :key="a.id"
        class="address-card"
        :class="{ selected: props.selectable && selected === a.id }"
      >
        <button
          v-if="props.selectable"
          class="address-select"
          @click="selected = a.id"
          :aria-label="'选择 ' + a.recipient + ' 的地址'"
        >
          <span
            class="radio-dot"
            :class="{ checked: selected === a.id }"
          ></span>
        </button>
        <div>
          <b
            >{{ a.recipient }}<span>{{ a.phone }}</span></b
          >
          <p>{{ a.region }}<br />{{ a.detail }}</p>
          <div class="address-actions">
            <button @click="edit(a)">编辑</button
            ><button @click="remove(a)">删除</button>
          </div>
        </div>
      </article>
    </div>
    <div v-if="!addresses.length && !editing" class="empty-inline">
      还没有收货地址，添加后即可下单。
    </div>
    <form v-if="editing" class="address-form" @submit.prevent="save">
      <h4>{{ form.id ? "编辑地址" : "新增收货地址" }}</h4>
      <div class="form-grid">
        <label
          >收件人<input
            v-model="form.recipient"
            maxlength="40"
            required
            placeholder="收件人姓名" /></label
        ><label
          >联系电话<input
            v-model="form.phone"
            minlength="6"
            maxlength="24"
            required
            placeholder="手机或固定电话"
        /></label>
      </div>
      <label
        >所在地区<input
          v-model="form.region"
          minlength="2"
          maxlength="80"
          required
          placeholder="省 / 市 / 区" /></label
      ><label
        >详细地址<input
          v-model="form.detail"
          minlength="3"
          maxlength="180"
          required
          placeholder="街道、楼栋、门牌号"
      /></label>
      <div class="button-row">
        <button class="btn dark" :disabled="busy">保存地址</button
        ><button type="button" class="btn outline" @click="editing = false">
          取消
        </button>
      </div>
    </form>
  </section>
</template>
