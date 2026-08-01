<template>
  <div class="space-y-6">
    <ResellerSectionHeader
      :title="t('resellerConsole.wholesale.title')"
      :description="t('resellerConsole.wholesale.description')"
    >
      <template #actions>
        <Button as-child variant="outline" size="sm">
          <RouterLink to="/reseller/orders">
            <ShoppingBag class="h-4 w-4" />
            {{ t('resellerConsole.wholesale.viewOrders') }}
          </RouterLink>
        </Button>
      </template>
    </ResellerSectionHeader>

    <Card class="p-5">
      <div class="flex flex-col gap-3 sm:flex-row sm:items-center">
        <Input
          v-model="keyword"
          class="sm:max-w-xs"
          :placeholder="t('resellerConsole.wholesale.searchPlaceholder')"
          @input="debouncedSearch"
        />
        <p class="text-sm text-muted-foreground">{{ t('resellerConsole.wholesale.listSummary', { total }) }}</p>
      </div>
    </Card>

    <Alert v-if="pageError" variant="destructive" class="items-start">
      <AlertDescription>{{ pageError }}</AlertDescription>
    </Alert>

    <div v-if="loading" class="space-y-4">
      <Card v-for="n in 3" :key="n" class="p-5">
        <div class="h-5 w-1/3 animate-pulse rounded bg-muted" />
        <div class="mt-4 h-16 animate-pulse rounded bg-muted" />
      </Card>
    </div>

    <div v-else-if="products.length === 0" class="rounded-xl border bg-muted/30 p-10 text-center">
      <p class="text-sm font-semibold text-foreground">{{ t('resellerConsole.wholesale.empty') }}</p>
      <p class="mt-1 text-sm text-muted-foreground">{{ t('resellerConsole.wholesale.emptyDescription') }}</p>
    </div>

    <Card v-for="product in products" :key="product.product.id" class="overflow-hidden">
      <div class="flex items-start justify-between gap-4 border-b p-5">
        <div class="min-w-0">
          <h2 class="truncate text-base font-bold text-foreground">{{ productTitle(product) }}</h2>
          <p class="mt-1 text-xs text-muted-foreground">
            {{ t('resellerConsole.wholesale.basePriceLabel') }}
            <span class="font-mono font-semibold text-foreground">{{ formatPrice(product.product.price_amount) }}</span>
          </p>
        </div>
        <Badge v-if="product.product.card_check_enabled" variant="accent" size="xs">
          {{ t('resellerConsole.wholesale.cardCheckSupported') }}
        </Badge>
      </div>

      <div class="divide-y">
        <div v-for="sku in product.skus" :key="sku.id" class="flex flex-wrap items-center gap-4 p-5">
          <div class="min-w-0 flex-1">
            <p class="truncate text-sm font-semibold text-foreground">{{ skuTitle(sku) }}</p>
            <div class="mt-1 flex flex-wrap gap-3 text-xs text-muted-foreground">
              <span>
                {{ t('resellerConsole.wholesale.basePrice') }}
                <span class="font-mono font-semibold">{{ formatPrice(sku.base_price_amount) }}</span>
              </span>
              <span class="text-primary">
                {{ t('resellerConsole.wholesale.channelPrice') }}
                <span class="font-mono font-bold">{{ formatPrice(effectivePrice(sku)) }}</span>
              </span>
              <span v-if="hasDiscount(sku)">
                {{ t('resellerConsole.wholesale.saveLabel') }}
                <span class="font-mono font-semibold text-success">{{ formatPrice(discountAmount(sku)) }}</span>
              </span>
            </div>
          </div>

          <label
            v-if="product.product.card_check_enabled"
            class="flex items-center gap-2 text-xs font-medium text-foreground"
          >
            <Checkbox
              :checked="isCardCheckEnabled(product.product.id, sku.id)"
              @update:checked="setCardCheck(product.product.id, sku.id, !!$event)"
            />
            {{ t('resellerConsole.wholesale.cardCheck') }}
          </label>

          <div class="flex items-center gap-1">
            <Button variant="outline" size="icon" class="h-8 w-8" @click="decrement(product.product.id, sku.id)">
              <Minus class="h-4 w-4" />
            </Button>
            <Input
              type="number"
              min="1"
              class="h-8 w-20 text-center font-mono"
              :model-value="quantity(product.product.id, sku.id)"
              @update:model-value="setQuantity(product.product.id, sku.id, $event)"
            />
            <Button variant="outline" size="icon" class="h-8 w-8" @click="increment(product.product.id, sku.id)">
              <Plus class="h-4 w-4" />
            </Button>
          </div>

          <div class="w-28 text-right">
            <p class="text-xs text-muted-foreground">{{ t('resellerConsole.wholesale.lineTotal') }}</p>
            <p class="font-mono text-base font-black text-foreground">{{ formatPrice(lineTotal(product, sku)) }}</p>
          </div>
        </div>
      </div>
    </Card>

    <Card v-if="selectedCount > 0" class="sticky bottom-4 border-primary/30 bg-card/95 p-5 shadow-lg backdrop-blur">
      <div class="flex flex-wrap items-center justify-between gap-4">
        <div>
          <p class="text-xs text-muted-foreground">{{ t('resellerConsole.wholesale.selectedItems', { count: selectedCount }) }}</p>
          <p class="mt-0.5 font-mono text-xl font-black text-foreground">{{ formatPrice(selectedTotal, previewCurrency) }}</p>
        </div>
        <div class="flex gap-2">
          <Button variant="outline" :disabled="previewLoading" @click="runPreview">
            {{ previewLoading ? t('resellerConsole.wholesale.previewing') : t('resellerConsole.wholesale.preview') }}
          </Button>
          <Button :disabled="previewLoading || creating" @click="submit">
            {{ creating ? t('resellerConsole.wholesale.submitting') : t('resellerConsole.wholesale.submit') }}
          </Button>
        </div>
      </div>

      <div v-if="preview" class="mt-4 rounded-xl border bg-muted/30 p-4">
        <div class="space-y-2">
          <div v-for="item in preview.items" :key="`${item.product_id}:${item.sku_id}`" class="flex items-center justify-between text-sm">
            <span class="text-muted-foreground">{{ t('resellerConsole.wholesale.previewItem', { quantity: item.quantity, unit: formatPrice(item.unit_price) }) }}</span>
            <span class="font-mono font-semibold text-foreground">{{ formatPrice(item.total_price) }}</span>
          </div>
          <div class="flex items-center justify-between border-t pt-2">
            <span class="text-sm font-semibold text-foreground">{{ t('resellerConsole.wholesale.total') }}</span>
            <span class="font-mono text-lg font-black text-foreground">{{ formatPrice(preview.total_amount, preview.currency) }}</span>
          </div>
        </div>
      </div>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { Minus, Plus, ShoppingBag } from 'lucide-vue-next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { resellerAPI } from '../../api/reseller'
import type {
  ResellerProductSettingDetailData,
  ResellerProductSettingSKUData,
  WholesalePurchasePreviewData,
} from '../../api/types'
import ResellerSectionHeader from '../../components/reseller-console/ResellerSectionHeader.vue'
import { useAppStore } from '../../stores/app'
import { useLocalized } from '../../composables/useProduct'
import { debounceAsync } from '../../utils/debounce'

interface CartLine {
  productId: number
  skuId: number
  skuTitle: string
  basePrice: string
  channelPrice: string
  quantity: number
  cardCheckEnabled: boolean
}

const { t } = useI18n()
const router = useRouter()
const appStore = useAppStore()
const { siteCurrency, formatPrice } = useLocalized()

const products = ref<ResellerProductSettingDetailData[]>([])
const total = ref(0)
const loading = ref(false)
const previewLoading = ref(false)
const creating = ref(false)
const pageError = ref('')
const preview = ref<WholesalePurchasePreviewData | null>(null)
const keyword = ref('')
const cart = ref<Record<string, CartLine>>({})

const previewCurrency = computed(() => preview.value?.currency || siteCurrency.value || 'CNY')

const productTitle = (product: ResellerProductSettingDetailData) => {
  const title = product.product.title
  return title?.[appStore.locale] || title?.['zh-CN'] || title?.['en-US'] || ''
}

const skuTitle = (sku: ResellerProductSettingSKUData) => {
  const spec = sku.spec_values || {}
  const values = Object.values(spec).filter((v) => v).join(' / ')
  return values ? `${sku.sku_code || ''} ${values}`.trim() : (sku.sku_code || '-')
}

const effectivePrice = (sku: ResellerProductSettingSKUData) =>
  sku.effective_price_amount && Number(sku.effective_price_amount) > 0 ? sku.effective_price_amount : sku.base_price_amount

const hasDiscount = (sku: ResellerProductSettingSKUData) =>
  Number(sku.effective_price_amount) > 0 && Number(effectivePrice(sku)) < Number(sku.base_price_amount)

const discountAmount = (sku: ResellerProductSettingSKUData) =>
  (Number(sku.base_price_amount) - Number(effectivePrice(sku))).toFixed(2)

const lineKey = (productId: number, skuId: number) => `${productId}:${skuId}`

const ensureLine = (product: ResellerProductSettingDetailData, sku: ResellerProductSettingSKUData): CartLine => {
  const key = lineKey(product.product.id, sku.id)
  let line = cart.value[key]
  if (!line) {
    line = {
      productId: product.product.id,
      skuId: sku.id,
      skuTitle: skuTitle(sku),
      basePrice: sku.base_price_amount,
      channelPrice: effectivePrice(sku),
      quantity: 1,
      cardCheckEnabled: product.product.card_check_enabled,
    }
    cart.value[key] = line
  }
  return line
}

const quantity = (productId: number, skuId: number) => {
  const line = cart.value[lineKey(productId, skuId)]
  return line ? line.quantity : 0
}

const increment = (productId: number, skuId: number) => {
  const line = cart.value[lineKey(productId, skuId)]
  if (line) line.quantity = Math.max(1, line.quantity + 1)
  invalidatePreview()
}

const decrement = (productId: number, skuId: number) => {
  const line = cart.value[lineKey(productId, skuId)]
  if (!line) return
  const next = line.quantity - 1
  if (next <= 0) {
    delete cart.value[lineKey(productId, skuId)]
  } else {
    line.quantity = next
  }
  invalidatePreview()
}

const setQuantity = (productId: number, skuId: number, raw: unknown) => {
  const line = cart.value[lineKey(productId, skuId)]
  if (!line) return
  const parsed = Math.max(1, Math.floor(Number(raw) || 1))
  line.quantity = Number.isFinite(parsed) ? parsed : 1
  invalidatePreview()
}

const isCardCheckEnabled = (productId: number, skuId: number) => {
  const line = cart.value[lineKey(productId, skuId)]
  return line ? line.cardCheckEnabled : false
}

const setCardCheck = (productId: number, skuId: number, enabled: boolean) => {
  const line = cart.value[lineKey(productId, skuId)]
  if (line) {
    line.cardCheckEnabled = enabled
    invalidatePreview()
  }
}

const selectedCount = computed(() =>
  Object.values(cart.value).reduce((sum, line) => sum + line.quantity, 0),
)

const selectedTotal = computed(() => {
  let sum = 0
  for (const line of Object.values(cart.value)) {
    sum += Number(line.channelPrice) * line.quantity
  }
  return sum.toFixed(2)
})

const lineTotal = (product: ResellerProductSettingDetailData, sku: ResellerProductSettingSKUData) => {
  const line = cart.value[lineKey(product.product.id, sku.id)]
  if (!line) return '0.00'
  return (Number(line.channelPrice) * line.quantity).toFixed(2)
}

const buildItemsPayload = () =>
  Object.values(cart.value).map((line) => ({
    product_id: line.productId,
    sku_id: line.skuId,
    quantity: line.quantity,
    card_check_enabled: line.cardCheckEnabled || undefined,
  }))

const invalidatePreview = () => {
  preview.value = null
}

const runPreview = async () => {
  const payload = { items: buildItemsPayload() }
  if (payload.items.length === 0) return
  previewLoading.value = true
  pageError.value = ''
  try {
    const response = await resellerAPI.purchasePreview(payload)
    preview.value = response.data.data
  } catch (err: any) {
    pageError.value = err?.message || t('resellerConsole.wholesale.previewFailed')
    preview.value = null
  } finally {
    previewLoading.value = false
  }
}

const submit = async () => {
  const payload = { items: buildItemsPayload() }
  if (payload.items.length === 0) return
  creating.value = true
  pageError.value = ''
  try {
    const response = await resellerAPI.purchaseCreate(payload)
    const data = response.data.data
    if (!data?.order_no) {
      throw new Error(t('resellerConsole.wholesale.submitFailed'))
    }
    cart.value = {}
    preview.value = null
    router.push(`/pay?order_no=${encodeURIComponent(data.order_no)}`)
  } catch (err: any) {
    pageError.value = err?.message || t('resellerConsole.wholesale.submitFailed')
  } finally {
    creating.value = false
  }
}

const debouncedSearch = debounceAsync(async () => {
  await loadCatalog(true)
}, 300)

const loadCatalog = async (keepKeyword = false) => {
  loading.value = true
  pageError.value = ''
  try {
    const response = await resellerAPI.catalog({ page: 1, page_size: 50, keyword: keyword.value })
    products.value = Array.isArray(response.data.data) ? response.data.data : []
    total.value = Number(response.data?.meta?.total ?? products.value.length)
    for (const product of products.value) {
      for (const sku of product.skus) {
        ensureLine(product, sku)
      }
    }
  } catch (err: any) {
    pageError.value = err?.message || t('resellerConsole.wholesale.loadFailed')
    products.value = []
  } finally {
    loading.value = false
    void keepKeyword
  }
}

onMounted(() => loadCatalog())
</script>
