<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI, type AdminCardBin, type AdminCardBinColumnMap } from '@/api/admin'
import { Upload, Trash2, RefreshCw, Database } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { confirmAction } from '@/utils/confirm'
import { formatDate } from '@/utils/format'

const { t } = useI18n()

const defaultColumnMap = (): AdminCardBinColumnMap => ({
  bin: 'BIN',
  country: 'isoCode2',
  brand: 'Brand',
  type: 'Type',
  prepaid: 'Category',
})

const defaultTypeRules: Record<string, string> = {
  CREDIT: 'C',
  CHARGE: 'C',
}

const defaultPrepaidKeywords = ['PREPAID']

const columnMap = reactive<AdminCardBinColumnMap>(defaultColumnMap())
const typeRules = reactive<Record<string, string>>({ ...defaultTypeRules })
const typeRuleSource = ref('')
const typeRuleTarget = ref<'D' | 'PD' | 'C'>('C')
const prepaidKeywords = ref<string[]>(defaultPrepaidKeywords)
const prepaidKeywordInput = ref('')

const fileInput = ref<HTMLInputElement | null>(null)
const uploading = ref(false)
const importResult = ref<{ total: number; inserted: number; skipped: number; countries: string[] } | null>(null)
const importError = ref('')

const stats = ref<{ total: number } | null>(null)
const statsLoading = ref(false)

const bins = ref<AdminCardBin[]>([])
const binsLoading = ref(false)
const total = ref(0)
const page = ref(1)
const pageSize = 50

const filters = reactive({ country: '', brand: '', keyword: '' })

const brandOptions = [
  { value: 'visa', label: 'Visa' },
  { value: 'mastercard', label: 'Mastercard' },
  { value: 'discover', label: 'Discover' },
  { value: 'other', label: 'Other' },
]

const typeOptions = [
  { value: 'D', label: 'D（含预付）' },
  { value: 'PD', label: '纯D（不含预付）' },
  { value: 'C', label: '纯C' },
]

const cardTypeLabel = (value: string) => {
  const matched = typeOptions.find((item) => item.value === value)
  return matched ? matched.label : value || '-'
}

const loadStats = async () => {
  statsLoading.value = true
  try {
    const response = await adminAPI.getCardBinStats()
    stats.value = response.data.data
  } catch {
    stats.value = null
  } finally {
    statsLoading.value = false
  }
}

const loadBins = async () => {
  binsLoading.value = true
  try {
    const response = await adminAPI.getCardBins({
      country: filters.country || undefined,
      brand: filters.brand || undefined,
      keyword: filters.keyword || undefined,
      offset: (page.value - 1) * pageSize,
      limit: pageSize,
    })
    bins.value = Array.isArray(response.data.data?.items) ? response.data.data.items : []
    total.value = Number(response.data.data?.total || 0)
  } catch {
    bins.value = []
    total.value = 0
  } finally {
    binsLoading.value = false
  }
}

const changePage = (delta: number) => {
  const next = page.value + delta
  if (next < 1) return
  page.value = next
  loadBins()
}

const applyFilters = () => {
  page.value = 1
  loadBins()
}

const resetFilters = () => {
  filters.country = ''
  filters.brand = ''
  filters.keyword = ''
  page.value = 1
  loadBins()
}

const refreshAll = () => {
  loadStats()
  loadBins()
}

const handleFileChange = async (event: Event) => {
  const target = event.target as HTMLInputElement
  const file = target.files?.[0]
  if (!file) return
  target.value = ''
  importError.value = ''
  importResult.value = null
  uploading.value = true
  try {
    const formData = new FormData()
    formData.append('file', file)
    formData.append('column_map', JSON.stringify(columnMap))
    formData.append('type_rules', JSON.stringify(typeRules))
    formData.append('prepaid_keywords', JSON.stringify(prepaidKeywords.value))
    const response = await adminAPI.importCardBins(formData)
    importResult.value = response.data.data
    await refreshAll()
  } catch (error: any) {
    importError.value = error?.message || t('admin.cardBins.importFailed')
  } finally {
    uploading.value = false
  }
}

const addTypeRule = () => {
  const source = typeRuleSource.value.trim().toUpperCase()
  if (!source) return
  typeRules[source] = typeRuleTarget.value
  typeRuleSource.value = ''
}

const addPrepaidKeyword = () => {
  const keyword = prepaidKeywordInput.value.trim().toUpperCase()
  if (!keyword) return
  if (!prepaidKeywords.value.includes(keyword)) {
    prepaidKeywords.value.push(keyword)
  }
  prepaidKeywordInput.value = ''
}

const removePrepaidKeyword = (keyword: string) => {
  prepaidKeywords.value = prepaidKeywords.value.filter((item) => item !== keyword)
}

const removeTypeRule = (source: string) => {
  delete typeRules[source]
}

const clearBins = async () => {
  const confirmed = await confirmAction({
    description: t('admin.cardBins.clearConfirm'),
    confirmText: t('admin.common.delete'),
    variant: 'destructive',
  })
  if (!confirmed) return
  try {
    await adminAPI.clearCardBins()
    await refreshAll()
  } catch {
    /* ignore */
  }
}

onMounted(() => {
  loadStats()
  loadBins()
})
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <h1 class="flex items-center gap-2 text-2xl font-semibold">
          <Database class="h-6 w-6 text-primary" />
          {{ t('admin.cardBins.title') }}
        </h1>
        <p v-if="statsLoading" class="mt-1 text-xs text-muted-foreground">{{ t('admin.common.loading') }}</p>
        <p v-else-if="stats" class="mt-1 text-xs text-muted-foreground">
          {{ t('admin.cardBins.stats', { total: stats.total }) }}
        </p>
      </div>
      <div class="flex flex-wrap gap-2">
        <Button size="sm" variant="outline" :disabled="uploading" @click="fileInput?.click()">
          <Upload class="mr-2 h-4 w-4" />
          {{ uploading ? t('admin.common.loading') : t('admin.cardBins.uploadAction') }}
        </Button>
        <input ref="fileInput" type="file" accept=".csv,text/csv" class="hidden" @change="handleFileChange" />
        <Button size="sm" variant="outline" @click="refreshAll">
          <RefreshCw class="mr-2 h-4 w-4" />
          {{ t('admin.common.refresh') }}
        </Button>
        <Button size="sm" variant="outline" class="border-destructive/40 text-destructive hover:bg-destructive/10" @click="clearBins">
          <Trash2 class="mr-2 h-4 w-4" />
          {{ t('admin.cardBins.clearAction') }}
        </Button>
      </div>
    </div>

    <div class="rounded-xl border border-border bg-card p-4 text-xs text-muted-foreground">
      <p>{{ t('admin.cardBins.helpDescription') }}</p>
      <p class="mt-1">{{ t('admin.cardBins.autoAnnotateHint') }}</p>
    </div>

    <div class="grid grid-cols-1 gap-6 xl:grid-cols-2">
      <div class="rounded-xl border border-border bg-card p-5">
        <h2 class="text-lg font-semibold text-foreground">{{ t('admin.cardBins.columnMapTitle') }}</h2>
        <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.cardBins.columnMapDesc') }}</p>
        <div class="mt-4 grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div class="space-y-1.5">
            <Label class="text-xs">{{ t('admin.cardBins.columns.bin') }}</Label>
            <Input v-model="columnMap.bin" />
          </div>
          <div class="space-y-1.5">
            <Label class="text-xs">{{ t('admin.cardBins.columns.country') }}</Label>
            <Input v-model="columnMap.country" />
          </div>
          <div class="space-y-1.5">
            <Label class="text-xs">{{ t('admin.cardBins.columns.brand') }}</Label>
            <Input v-model="columnMap.brand" />
          </div>
          <div class="space-y-1.5">
            <Label class="text-xs">{{ t('admin.cardBins.columns.type') }}</Label>
            <Input v-model="columnMap.type" />
          </div>
          <div class="space-y-1.5">
            <Label class="text-xs">{{ t('admin.cardBins.columns.prepaid') }}</Label>
            <Input v-model="columnMap.prepaid" />
          </div>
        </div>
        <p class="mt-4 text-xs text-muted-foreground">{{ t('admin.cardBins.columnMapDefaultHint') }}</p>
      </div>

      <div class="rounded-xl border border-border bg-card p-5">
        <h2 class="text-lg font-semibold text-foreground">{{ t('admin.cardBins.typeRulesTitle') }}</h2>
        <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.cardBins.typeRulesDesc') }}</p>
        <div class="mt-4 flex flex-wrap items-end gap-2">
          <div class="flex-1 space-y-1.5">
            <Label class="text-xs">{{ t('admin.cardBins.typeRulesSource') }}</Label>
            <Input v-model="typeRuleSource" :placeholder="t('admin.cardBins.typeRulesSourcePlaceholder')" @keyup.enter="addTypeRule" />
          </div>
          <div class="space-y-1.5">
            <Label class="text-xs">{{ t('admin.cardBins.typeRulesTarget') }}</Label>
            <Select :model-value="typeRuleTarget" @update:model-value="(v: unknown) => (typeRuleTarget = v as 'D' | 'PD' | 'C')">
              <SelectTrigger class="w-40">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="item in typeOptions" :key="item.value" :value="item.value">{{ item.label }}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <Button size="sm" @click="addTypeRule">{{ t('admin.cardBins.typeRulesAdd') }}</Button>
        </div>
        <div class="mt-4 flex flex-wrap gap-2">
          <span
            v-for="(target, source) in typeRules"
            :key="source"
            class="inline-flex items-center gap-2 rounded-full border border-border bg-muted/30 px-3 py-1 text-xs"
          >
            <span class="font-mono">{{ source }}</span>
            <span class="text-muted-foreground">→</span>
            <span>{{ cardTypeLabel(target) }}</span>
            <button type="button" class="text-muted-foreground hover:text-destructive" @click="removeTypeRule(source)">×</button>
          </span>
        </div>
        <p class="mt-4 text-xs text-muted-foreground">{{ t('admin.cardBins.typeRulesHint') }}</p>
        <div class="mt-4 border-t border-border pt-4">
          <div class="flex flex-wrap items-end gap-2">
            <div class="flex-1 space-y-1.5">
              <Label class="text-xs">{{ t('admin.cardBins.prepaidLabel') }}</Label>
              <Input v-model="prepaidKeywordInput" :placeholder="t('admin.cardBins.prepaidPlaceholder')" @keyup.enter="addPrepaidKeyword" />
            </div>
            <Button size="sm" variant="outline" @click="addPrepaidKeyword">{{ t('admin.cardBins.prepaidAdd') }}</Button>
          </div>
          <p class="mt-2 text-xs text-muted-foreground">{{ t('admin.cardBins.prepaidHint') }}</p>
          <div class="mt-3 flex flex-wrap gap-2">
            <span
              v-for="keyword in prepaidKeywords"
              :key="keyword"
              class="inline-flex items-center gap-2 rounded-full border border-border bg-muted/30 px-3 py-1 text-xs"
            >
              <span class="font-mono">{{ keyword }}</span>
              <button type="button" class="text-muted-foreground hover:text-destructive" @click="removePrepaidKeyword(keyword)">×</button>
            </span>
          </div>
        </div>
      </div>
    </div>

    <div v-if="importResult" class="rounded-xl border border-emerald-200 bg-emerald-50 p-4 text-sm text-emerald-700">
      {{ t('admin.cardBins.importSuccess', { total: importResult.total, inserted: importResult.inserted, skipped: importResult.skipped }) }}
      <span v-if="importResult.countries.length" class="ml-2 text-xs">
        {{ t('admin.cardBins.importCountries') }}：{{ importResult.countries.join(' / ') }}
      </span>
    </div>
    <div v-if="importError" class="rounded-xl border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive">
      {{ importError }}
    </div>

    <div class="rounded-xl border border-border bg-card p-5">
      <div class="mb-4 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <h2 class="text-lg font-semibold text-foreground">{{ t('admin.cardBins.listTitle') }}</h2>
          <p class="text-xs text-muted-foreground">{{ t('admin.cardBins.listTotal', { total }) }}</p>
        </div>
        <div class="grid w-full grid-cols-2 gap-3 md:grid-cols-4 lg:max-w-3xl">
          <Input v-model="filters.country" :placeholder="t('admin.cardBins.filterCountry')" @keyup.enter="applyFilters" />
          <Select :model-value="filters.brand" @update:model-value="(v: unknown) => (filters.brand = String(v ?? ''))">
            <SelectTrigger class="h-10">
              <SelectValue :placeholder="t('admin.cardBins.brandAll')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="">{{ t('admin.cardBins.brandAll') }}</SelectItem>
              <SelectItem v-for="item in brandOptions" :key="item.value" :value="item.value">{{ item.label }}</SelectItem>
            </SelectContent>
          </Select>
          <Input v-model="filters.keyword" :placeholder="t('admin.cardBins.filterKeyword')" @keyup.enter="applyFilters" />
          <div class="flex gap-2">
            <Button class="flex-1" variant="outline" @click="applyFilters">{{ t('admin.common.search') }}</Button>
            <Button class="flex-1" variant="outline" @click="resetFilters">{{ t('admin.common.reset') }}</Button>
          </div>
        </div>
      </div>

      <div class="overflow-x-auto">
        <Table>
          <TableHeader class="border-b border-border bg-muted/40 text-xs uppercase text-muted-foreground">
            <TableRow>
              <TableHead class="px-4 py-3">{{ t('admin.cardBins.listTable.bin') }}</TableHead>
              <TableHead class="px-4 py-3">{{ t('admin.cardBins.listTable.country') }}</TableHead>
              <TableHead class="px-4 py-3">{{ t('admin.cardBins.listTable.brand') }}</TableHead>
              <TableHead class="px-4 py-3">{{ t('admin.cardBins.listTable.cardType') }}</TableHead>
              <TableHead class="px-4 py-3">{{ t('admin.cardBins.listTable.issuer') }}</TableHead>
              <TableHead class="px-4 py-3">{{ t('admin.cardBins.listTable.updatedAt') }}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody class="divide-y divide-border">
            <TableRow v-if="binsLoading">
              <TableCell colspan="6" class="px-4 py-8 text-center text-muted-foreground">{{ t('admin.common.loading') }}</TableCell>
            </TableRow>
            <TableRow v-else-if="bins.length === 0">
              <TableCell colspan="6" class="px-4 py-8 text-center text-muted-foreground">{{ t('admin.cardBins.emptyList') }}</TableCell>
            </TableRow>
            <TableRow v-for="bin in bins" :key="bin.bin" class="hover:bg-muted/30">
              <TableCell class="px-4 py-3 font-mono text-xs">{{ bin.bin }}</TableCell>
              <TableCell class="px-4 py-3 text-xs">{{ bin.country || '-' }}</TableCell>
              <TableCell class="px-4 py-3 text-xs">{{ bin.raw_brand || bin.brand || '-' }}</TableCell>
              <TableCell class="px-4 py-3 text-xs">{{ cardTypeLabel(bin.card_type) }}</TableCell>
              <TableCell class="px-4 py-3 text-xs text-muted-foreground">{{ bin.issuer || '-' }}</TableCell>
              <TableCell class="px-4 py-3 text-xs text-muted-foreground">{{ formatDate(bin.updated_at) }}</TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </div>

      <div class="mt-4 flex items-center justify-between text-xs text-muted-foreground">
        <span>{{ t('admin.cardBins.listTotal', { total }) }}</span>
        <div class="flex items-center gap-2">
          <Button size="sm" variant="outline" :disabled="page <= 1" @click="changePage(-1)">{{ t('admin.cardBins.pagination.prev') }}</Button>
          <span>{{ page }}</span>
          <Button size="sm" variant="outline" :disabled="page * pageSize >= total" @click="changePage(1)">{{ t('admin.cardBins.pagination.next') }}</Button>
        </div>
      </div>
    </div>
  </div>
</template>
