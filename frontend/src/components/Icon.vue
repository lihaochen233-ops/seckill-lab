<script setup lang="ts">
withDefaults(defineProps<{ name: string; size?: number }>(), { size: 20 });
const paths: Record<string, string> = {
  search: "m21 21-5-5 M19 10.5a8.5 8.5 0 1 1-17 0 8.5 8.5 0 0 1 17 0",
  bag: "M5 7h14l1 14H4L5 7Z M8 8V6a4 4 0 0 1 8 0v2",
  user: "M20 21v-2a7 7 0 0 0-14 0v2 M16 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0",
  arrow: "M4 12h16 m-6-6 6 6-6 6",
  bolt: "m13 2-9 12h7l-1 8 10-13h-7l0-7Z",
  check: "m5 12 4 4L19 6",
  close: "m6 6 12 12 M6 18 18 6",
  shield: "m12 2 9 4v6c0 6-9 10-9 10S3 18 3 12V6l9-4Z m-4 10 3 3 5-6",
  box: "m12 3 9 5-9 5-9-5 9-5Z M3 8v9l9 5 9-5V8 M12 13v9 m-5-17 9 5",
  clock: "M22 12A10 10 0 1 1 2 12a10 10 0 0 1 20 0 M12 6v6l4 2",
  heart:
    "M20.8 4.6a5.5 5.5 0 0 0-7.8 0L12 5.7l-1.1-1.1a5.5 5.5 0 0 0-7.8 7.8L12 21l8.8-8.6a5.5 5.5 0 0 0 0-7.8Z",
  grid: "M3 3h7v7H3z M14 3h7v7h-7z M3 14h7v7H3z M14 14h7v7h-7z",
  logout: "M9 21H4V3h5 m7 4 5 5-5 5 M8 12h13",
  plus: "M12 5v14 M5 12h14",
  minus: "M5 12h14",
  trash: "M3 6h18 M9 6V3h6v3 M5 6l1 15h12l1-15 M10 10v7 M14 10v7",
  refresh: "M20 7a9 9 0 1 0 1 8 M20 2v6h-6",
  chevron: "m9 5 7 7-7 7",
  location:
    "M20 10c0 6-8 12-8 12S4 16 4 10a8 8 0 1 1 16 0 M15 10a3 3 0 1 1-6 0 3 3 0 0 1 6 0",
  chart: "M4 20V10 M10 20V4 M16 20V8 M22 20V2",
  mail: "M3 5h18v14H3z m0 0 9 7 9-7",
  menu: "M3 6h18 M3 12h18 M3 18h18",
};
</script>
<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.65"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path :d="paths[name] || paths.box" />
  </svg>
</template>
