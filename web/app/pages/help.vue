<script setup lang="ts">
import { marked } from 'marked'
// Single source: the same Markdown file is the repository's user guide.
import guide from '../../../docs/huong-dan-su-dung.md?raw'

useHead({ title: 'Hướng dẫn sử dụng' })

// Headings written as "## Tiêu đề {#neo}" get id="neo" so links such as
// /help#ket-noi-shop jump to the right place. Image paths in the guide are
// relative to docs/ ("huong-dan/x.png"), served at /huong-dan/x.png.
const html = computed(() => {
  const md = guide
    .replace(/^(#{1,4}) (.+?) \{#([a-z0-9-]+)\}$/gm, (_m, hashes: string, title: string, id: string) => `<h${hashes.length} id="${id}">${title}</h${hashes.length}>`)
    .replace(/\]\(huong-dan\//g, '](/huong-dan/')
  return marked.parse(md, { async: false }) as string
})

const route = useRoute()
onMounted(async () => {
  if (route.hash) {
    await nextTick()
    document.getElementById(route.hash.slice(1))?.scrollIntoView()
  }
})
</script>

<template>
  <div class="mx-auto max-w-3xl">
    <!-- The guide is a file from this repository, not user input. -->
    <!-- eslint-disable-next-line vue/no-v-html -->
    <article class="guide" v-html="html" />
  </div>
</template>

<style scoped>
.guide :deep(h1) { font-size: 1.75rem; font-weight: 600; margin-bottom: 1rem; }
.guide :deep(h2) { font-size: 1.25rem; font-weight: 600; margin-top: 2.25rem; margin-bottom: .75rem; scroll-margin-top: 1rem; }
.guide :deep(h3) { font-size: 1.05rem; font-weight: 600; margin-top: 1.5rem; margin-bottom: .5rem; }
.guide :deep(p), .guide :deep(ul), .guide :deep(ol), .guide :deep(table), .guide :deep(blockquote) { margin-bottom: .9rem; line-height: 1.65; }
.guide :deep(ul) { list-style: disc; padding-left: 1.5rem; }
.guide :deep(ol) { list-style: decimal; padding-left: 1.5rem; }
.guide :deep(li) { margin-bottom: .3rem; }
.guide :deep(a) { color: var(--color-primary); text-decoration: underline; text-underline-offset: 3px; }
.guide :deep(code) { background: var(--color-muted); padding: .1rem .3rem; border-radius: .25rem; font-size: .9em; }
.guide :deep(blockquote) { border-left: 3px solid var(--color-primary); background: var(--color-muted); padding: .6rem .9rem; border-radius: .25rem; }
.guide :deep(blockquote p) { margin: 0; }
.guide :deep(table) { width: 100%; border-collapse: collapse; font-size: .9rem; }
.guide :deep(th), .guide :deep(td) { border: 1px solid var(--color-border); padding: .4rem .6rem; text-align: left; vertical-align: top; }
.guide :deep(th) { background: var(--color-muted); }
.guide :deep(img) { max-width: 100%; border: 1px solid var(--color-border); border-radius: .5rem; margin: .5rem 0; }
</style>
