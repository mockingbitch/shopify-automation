<script setup lang="ts">
// Renders a form for a JSON Schema object. Supports the subset used by
// template sections: strings (text, multiline, enum), numbers, booleans,
// string arrays (enum checkboxes or one value per line), arrays of objects
// and nested objects. Errors are shown by API field path.
import { labelOf, type FieldErrors, type JsonObject, type JsonSchema } from '~/utils/schema'

const props = defineProps<{
  schema: JsonSchema
  modelValue: JsonObject
  path: string // error path prefix, e.g. "sections.general"
  idPrefix: string
  errors?: FieldErrors
  readonly?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [JsonObject] }>()

const fields = computed(() => Object.entries(props.schema.properties ?? {}))

function set(name: string, value: unknown) {
  emit('update:modelValue', { ...props.modelValue, [name]: value })
}
function errorFor(name: string) {
  return props.errors?.[`${props.path}.${name}`]
}
const fid = (name: string) => `${props.idPrefix}-${name}`

function num(e: Event, integer: boolean): number | undefined {
  const raw = (e.target as HTMLInputElement).value
  if (raw === '') return undefined
  const n = integer ? Number.parseInt(raw, 10) : Number.parseFloat(raw)
  return Number.isNaN(n) ? undefined : n
}

function lines(v: unknown): string {
  return Array.isArray(v) ? v.join('\n') : ''
}
function fromLines(e: Event): string[] {
  return (e.target as HTMLTextAreaElement).value.split('\n').map((l) => l.trim()).filter(Boolean)
}

function toggle(name: string, option: string, checked: boolean) {
  const cur = Array.isArray(props.modelValue[name]) ? [...(props.modelValue[name] as string[])] : []
  set(name, checked ? [...new Set([...cur, option])] : cur.filter((x) => x !== option))
}

function rows(name: string): JsonObject[] {
  const v = props.modelValue[name]
  return Array.isArray(v) ? (v as JsonObject[]) : []
}
function setRow(name: string, i: number, row: JsonObject) {
  const next = [...rows(name)]
  next[i] = row
  set(name, next)
}
function addRow(name: string) {
  set(name, [...rows(name), {}])
}
function removeRow(name: string, i: number) {
  set(name, rows(name).filter((_, j) => j !== i))
}
function obj(name: string): JsonObject {
  const v = props.modelValue[name]
  return v && typeof v === 'object' && !Array.isArray(v) ? (v as JsonObject) : {}
}
</script>

<template>
  <div class="grid gap-4">
    <div v-for="[name, s] in fields" :key="name" class="space-y-1.5">
      <!-- Nested object -->
      <fieldset v-if="s.type === 'object'" class="rounded-md border p-3">
        <legend class="px-1 text-sm font-medium">
          {{ labelOf(name, s) }}
          <Badge v-if="s['x-sensitive']" variant="outline" class="ml-1">Sensitive</Badge>
        </legend>
        <p v-if="s.description" class="mb-2 text-xs text-muted-foreground">{{ s.description }}</p>
        <SchemaForm :schema="s" :model-value="obj(name)" :path="`${path}.${name}`" :id-prefix="fid(name)"
                    :errors="errors" :readonly="readonly" @update:model-value="set(name, $event)" />
        <p v-if="errorFor(name)" class="mt-1 text-xs text-destructive">{{ errorFor(name) }}</p>
      </fieldset>

      <!-- Array of objects -->
      <fieldset v-else-if="s.type === 'array' && s.items?.type === 'object'" class="rounded-md border p-3">
        <legend class="px-1 text-sm font-medium">
          {{ labelOf(name, s) }}
          <Badge v-if="s['x-sensitive']" variant="outline" class="ml-1">Sensitive</Badge>
        </legend>
        <p v-if="s.description" class="mb-2 text-xs text-muted-foreground">{{ s.description }}</p>
        <p v-if="!rows(name).length" class="text-sm text-muted-foreground">None.</p>
        <div v-for="(row, i) in rows(name)" :key="i" class="mb-3 rounded border bg-muted/30 p-3">
          <SchemaForm :schema="s.items!" :model-value="row" :path="`${path}.${name}.${i}`" :id-prefix="`${fid(name)}-${i}`"
                      :errors="errors" :readonly="readonly" @update:model-value="setRow(name, i, $event)" />
          <p v-if="errors?.[`${path}.${name}.${i}`]" class="mt-1 text-xs text-destructive">{{ errors[`${path}.${name}.${i}`] }}</p>
          <Button v-if="!readonly" type="button" variant="ghost" size="sm" class="mt-2" :aria-label="`Remove item ${i + 1} from ${labelOf(name, s)}`" @click="removeRow(name, i)">Remove</Button>
        </div>
        <Button v-if="!readonly && (s.maxItems === undefined || rows(name).length < s.maxItems)" type="button" variant="outline" size="sm" :aria-label="`Add to ${labelOf(name, s)}`" @click="addRow(name)">
          Add
        </Button>
        <p v-if="errorFor(name)" class="mt-1 text-xs text-destructive">{{ errorFor(name) }}</p>
      </fieldset>

      <!-- Scalars and string arrays -->
      <template v-else>
        <Label :for="fid(name)">
          {{ labelOf(name, s) }}
          <Badge v-if="s['x-sensitive']" variant="outline" class="ml-1">Sensitive</Badge>
        </Label>

        <div v-if="s.type === 'array' && s.items?.enum" :id="fid(name)" role="group" class="flex flex-wrap gap-4">
          <label v-for="opt in s.items.enum" :key="String(opt)" class="flex items-center gap-2 text-sm">
            <input type="checkbox" class="size-4" :disabled="readonly"
                   :checked="Array.isArray(modelValue[name]) && (modelValue[name] as string[]).includes(String(opt))"
                   @change="toggle(name, String(opt), ($event.target as HTMLInputElement).checked)">
            {{ opt }}
          </label>
        </div>
        <textarea v-else-if="s.type === 'array'" :id="fid(name)" rows="3" :readonly="readonly"
                  class="w-full rounded-md border bg-background px-3 py-2 text-sm" placeholder="One per line"
                  :value="lines(modelValue[name])" @change="set(name, fromLines($event))" />
        <select v-else-if="s.enum" :id="fid(name)" :disabled="readonly" class="h-9 w-full rounded-md border bg-background px-3 text-sm"
                :value="modelValue[name] ?? ''" @change="set(name, ($event.target as HTMLSelectElement).value || undefined)">
          <option value="">Not set</option>
          <option v-for="opt in s.enum" :key="String(opt)" :value="opt">{{ opt }}</option>
        </select>
        <label v-else-if="s.type === 'boolean'" class="flex items-center gap-2 text-sm">
          <input :id="fid(name)" type="checkbox" class="size-4" :disabled="readonly" :checked="modelValue[name] === true"
                 @change="set(name, ($event.target as HTMLInputElement).checked)">
          Yes
        </label>
        <Input v-else-if="s.type === 'integer' || s.type === 'number'" :id="fid(name)" type="number" :readonly="readonly"
               :step="s.type === 'integer' ? 1 : 'any'" :model-value="(modelValue[name] as number | undefined) ?? ''"
               @change="set(name, num($event, s.type === 'integer'))" />
        <textarea v-else-if="s['x-multiline']" :id="fid(name)" rows="4" :readonly="readonly" :maxlength="s.maxLength"
                  class="w-full rounded-md border bg-background px-3 py-2 text-sm"
                  :value="(modelValue[name] as string | undefined) ?? ''" @input="set(name, ($event.target as HTMLTextAreaElement).value)" />
        <Input v-else :id="fid(name)" :readonly="readonly" :maxlength="s.maxLength"
               :type="s.format === 'email' ? 'email' : s.format === 'uri' ? 'url' : 'text'"
               :model-value="(modelValue[name] as string | undefined) ?? ''" @update:model-value="set(name, $event)" />

        <p v-if="s.description && !errorFor(name)" class="text-xs text-muted-foreground">{{ s.description }}</p>
        <p v-if="errorFor(name)" class="text-xs text-destructive" role="alert">{{ errorFor(name) }}</p>
      </template>
    </div>
  </div>
</template>
