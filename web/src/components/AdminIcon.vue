<script setup lang="ts">
/**
 * The console's only icon source: a self-contained inline SVG set so the build
 * never depends on an icon font, sprite file or CDN. Every glyph is drawn on the
 * same 24×24 grid with `currentColor` strokes, so it inherits text colour and
 * scales with the `size` prop.
 */
import { computed } from 'vue'

const props = withDefaults(defineProps<{ name: string; size?: number | string }>(), {
  size: 18,
})

/** `name` → stroke paths on a 24×24 grid. */
const ICONS: Record<string, string[]> = {
  logo: ['M12 3v12m-5-5 5 5 5-5M5 21h14'],
  overview: ['M4 4h5v5H4z', 'M15 4h5v5h-5z', 'M4 15h5v5H4z', 'M15 15h5v5h-5z'],
  release: ['M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18Z', 'm8 12 3 3 5-6'],
  package: ['m21 8-9-5-9 5v8l9 5 9-5Z', 'M3 8l9 5 9-5', 'M12 13v8', 'M7.5 5.5l9 5'],
  upload: ['M12 15V3', 'm7 8 5-5 5 5', 'M4 15v4a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-4'],
  device: ['M9 2h6a3 3 0 0 1 3 3v14a3 3 0 0 1-3 3H9a3 3 0 0 1-3-3V5a3 3 0 0 1 3-3Z', 'M10 5h4', 'M11 19h2'],
  storage: ['M4 15 6 4h12l2 11', 'M4 15h16v5H4Z', 'M7 17.5h.01', 'M10 17.5h.01'],
  lock: ['M6 11h12a2 2 0 0 1 2 2v6a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2v-6a2 2 0 0 1 2-2Z', 'M8 11V7a4 4 0 0 1 8 0v4', 'M12 15v2'],
  refresh: ['M21 12a9 9 0 1 1-3-6.7L21 8', 'M21 3v5h-5'],
  logout: ['M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4m7 14 5-5-5-5M21 12H9'],
  plus: ['M12 5v14', 'M5 12h14'],
  info: ['M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18Z', 'M12 11v6', 'M12 7h.01'],
  close: ['m6 6 12 12', 'M6 18 18 6'],
  check: ['m5 13 4 4L19 7'],
  edit: ['M4 20h4L20 8l-4-4L4 16v4Z', 'm14 6 4 4'],
  trash: [
    'M4 7h16',
    'M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2',
    'M6 7l1 12a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2l1-12',
    'M10 11v6',
    'M14 11v6',
  ],
  calendar: [
    'M4 6h16a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1Z',
    'M3 10h18',
    'M8 3v4',
    'M16 3v4',
  ],
  clock: ['M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18Z', 'M12 8v4l3 2'],
  external: ['M14 4h6v6', 'M20 4 10 14', 'M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5'],
  download: ['M12 3v12', 'm7 10 5 5 5-5', 'M5 21h14'],
  'chevron-left': ['m14 6-6 6 6 6'],
  'chevron-right': ['m10 6 6 6-6 6'],
  'chevron-down': ['m6 9 6 6 6-6'],
  shield: ['M12 3 4 6v6c0 5 3.5 8 8 9 4.5-1 8-4 8-9V6l-8-3Z', 'm9 12 2 2 4-4'],
  user: ['M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8Z', 'M4 21a8 8 0 0 1 16 0'],
  warning: ['M12 4 3 20h18L12 4Z', 'M12 10v4', 'M12 17h.01'],
  link: [
    'M10 13a5 5 0 0 0 7 0l2-2a5 5 0 0 0-7-7l-1 1',
    'M14 11a5 5 0 0 0-7 0l-2 2a5 5 0 0 0 7 7l1-1',
  ],
  key: ['M15 7a4 4 0 1 0 0 8 4 4 0 0 0 0-8Z', 'm11.6 11.6L4 19.2V21h2.4l1.2-1.2h1.8v-1.8h1.8Z'],
}

const paths = computed(() => ICONS[props.name] ?? [])
const dimension = computed(() => (typeof props.size === 'number' ? `${props.size}px` : props.size))
</script>

<template>
  <svg
    class="admin-icon"
    :style="{ width: dimension, height: dimension }"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.7"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
  >
    <path v-for="(d, index) in paths" :key="index" :d="d" />
  </svg>
</template>
