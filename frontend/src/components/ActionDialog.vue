<script setup lang="ts">
import { nextTick, ref, watch } from "vue";
import { dialogState, resolveDialog } from "../dialog";
const element = ref<HTMLDialogElement>();
watch(
  () => dialogState.open,
  async (open) => {
    await nextTick();
    if (open) element.value?.showModal();
    else element.value?.close();
  },
);
</script>
<template>
  <Teleport to="body">
    <dialog
      ref="element"
      class="action-dialog"
      aria-labelledby="action-title"
      aria-describedby="action-message"
      @cancel.prevent="resolveDialog(false)"
    >
      <form @submit.prevent="resolveDialog(true)">
        <span class="eyebrow">PULSE / 请确认</span>
        <h2 id="action-title">
          {{ dialogState.input ? "填写操作信息" : "确认操作" }}
        </h2>
        <p id="action-message">{{ dialogState.message }}</p>
        <label v-if="dialogState.input"
          >操作内容<input
            v-model="dialogState.value"
            maxlength="100"
            required
            autofocus
        /></label>
        <div class="button-row">
          <button
            type="button"
            class="btn outline"
            @click="resolveDialog(false)"
          >
            暂不操作
          </button>
          <button type="submit" class="btn dark">确认继续</button>
        </div>
      </form>
    </dialog>
  </Teleport>
</template>
