<script setup lang="ts">
import type { components } from '~/types/api'

useHead({ title: 'Browser recipes' })

type Recipe = components['schemas']['BrowserRecipe']

const api = useApi()
const recipes = ref<Recipe[]>([])
const loading = ref(true)
const error = ref('')
const notice = ref('')
const fields = ref<Record<string, string>>({})
const busy = ref(false)
const text = ref('')
const notes = ref('')
const open = ref<string | null>(null)

async function load() {
  loading.value = true
  error.value = ''
  try {
    recipes.value = unwrap(await api.GET('/admin/browser-recipes')).data
  } catch (e) {
    error.value = toApiError(e).status === 404 ? 'This page is only available to platform administrators.' : toApiError(e).message
  } finally {
    loading.value = false
  }
}
onMounted(load)

async function create() {
  busy.value = true
  error.value = ''
  fields.value = {}
  let recipe: Record<string, unknown>
  try {
    recipe = JSON.parse(text.value)
  } catch {
    error.value = 'The recipe is not valid JSON.'
    busy.value = false
    return
  }
  try {
    unwrap(await api.POST('/admin/browser-recipes', { body: { recipe, notes: notes.value } }))
    notice.value = 'Recipe added. It stays disabled until it is verified on a real seller account and enabled.'
    text.value = ''
    notes.value = ''
    await load()
  } catch (e) {
    const err = toApiError(e)
    fields.value = err.fields
    error.value = Object.keys(err.fields).length ? 'The recipe has problems:' : err.message
  } finally {
    busy.value = false
  }
}

async function update(r: Recipe, body: { verified?: boolean; enabled?: boolean }, done: string) {
  error.value = ''
  try {
    unwrap(await api.PATCH('/admin/browser-recipes/{recipeID}', { params: { path: { recipeID: r.id } }, body }))
    notice.value = done
    await load()
  } catch (e) {
    error.value = toApiError(e).message
  }
}
const fmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' })
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-6">
    <PageHeader title="Browser recipes"
                description="Declarative steps the browser extension runs in Seller Centre. A version can be enabled only after it was verified on a real seller account; disabling is the kill switch." />
    <div aria-live="polite"><Alert v-if="notice"><AlertDescription>{{ notice }}</AlertDescription></Alert></div>
    <FormError :message="error" />
    <ul v-if="Object.keys(fields).length" class="list-disc pl-5 text-sm text-destructive">
      <li v-for="(m, f) in fields" :key="f"><span class="font-mono">{{ f }}</span>: {{ m }}</li>
    </ul>

    <form class="space-y-3 rounded-lg border bg-background p-4" @submit.prevent="create">
      <h2 class="text-base font-semibold">Add a version</h2>
      <FormField id="rc-json" label="Recipe JSON" hint="Schema: internal/browser/recipe.schema.json. Saved byte for byte, because those bytes are signed.">
        <textarea id="rc-json" v-model="text" rows="12" spellcheck="false" class="w-full rounded-md border bg-background px-3 py-2 font-mono text-xs" />
      </FormField>
      <FormField id="rc-notes" label="Notes (optional)"><Input id="rc-notes" v-model="notes" maxlength="2000" /></FormField>
      <div class="flex justify-end"><Button type="submit" :disabled="busy || !text.trim()">Add version</Button></div>
    </form>

    <p v-if="loading && !recipes.length" role="status" class="text-sm text-muted-foreground">Loading…</p>
    <EmptyState v-else-if="!recipes.length" title="No recipes" description="Operations without an Open API stay manual tasks until a verified recipe is enabled." />
    <div v-else class="overflow-x-auto rounded-lg border bg-background">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Capability</TableHead><TableHead>Market</TableHead><TableHead>Version</TableHead><TableHead>State</TableHead>
            <TableHead>Runs</TableHead><TableHead><span class="sr-only">Actions</span></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <template v-for="r in recipes" :key="r.id">
            <TableRow>
              <TableCell class="font-mono text-xs">{{ r.capability }}</TableCell>
              <TableCell>{{ r.market }}</TableCell>
              <TableCell>v{{ r.version }}</TableCell>
              <TableCell class="space-y-1">
                <Badge :variant="r.enabled ? 'default' : 'secondary'">{{ r.enabled ? 'Enabled' : 'Disabled' }}</Badge>
                <div class="text-xs text-muted-foreground">{{ r.verified_at ? `Verified ${fmt.format(new Date(r.verified_at))}` : 'Not verified' }}</div>
                <div v-if="r.disabled_reason" class="text-xs text-muted-foreground">{{ r.disabled_reason }}</div>
              </TableCell>
              <TableCell class="text-sm">{{ r.success_count }} ok · {{ r.failure_count }} failed</TableCell>
              <TableCell class="space-x-2 whitespace-nowrap text-right">
                <Button variant="outline" size="sm" @click="open = open === r.id ? null : r.id">{{ open === r.id ? 'Hide' : 'View' }}</Button>
                <ConfirmDialog v-if="!r.verified_at" title="Mark as verified?"
                               description="Confirm that this exact version ran successfully on a real seller account in this market."
                               confirm-label="Mark verified" @confirm="update(r, { verified: true }, 'Marked as verified.')">
                  <template #trigger><Button variant="outline" size="sm">Verify</Button></template>
                </ConfirmDialog>
                <Button v-if="r.verified_at && !r.enabled" size="sm" @click="update(r, { enabled: true }, 'Enabled. Other versions were disabled.')">Enable</Button>
                <Button v-if="r.enabled" variant="outline" size="sm" @click="update(r, { enabled: false }, 'Disabled. Tasks fall back to manual work.')">Disable</Button>
              </TableCell>
            </TableRow>
            <TableRow v-if="open === r.id">
              <TableCell colspan="6">
                <pre class="max-h-96 overflow-auto rounded bg-muted p-3 text-xs">{{ JSON.stringify(r.recipe, null, 2) }}</pre>
                <p v-if="r.notes" class="mt-2 text-sm">{{ r.notes }}</p>
              </TableCell>
            </TableRow>
          </template>
        </TableBody>
      </Table>
    </div>
  </div>
</template>
