import { reactive } from "vue";

export const dialogState = reactive({
  open: false,
  message: "",
  input: false,
  value: "",
});
let finish: ((value: string | null) => void) | undefined;

function ask(
  message: string,
  input: boolean,
  value = "",
): Promise<string | null> {
  if (finish) finish(null);
  Object.assign(dialogState, { open: true, message, input, value });
  return new Promise((resolve) => {
    finish = resolve;
  });
}
export async function confirmAction(message: string) {
  return (await ask(message, false)) !== null;
}
export function promptAction(message: string, value = "") {
  return ask(message, true, value);
}
export function resolveDialog(accepted: boolean) {
  const resolve = finish;
  finish = undefined;
  dialogState.open = false;
  resolve?.(accepted ? dialogState.value : null);
}
