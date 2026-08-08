import { ref, onMounted, onUnmounted, computed, watch, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useHead } from '@unhead/vue'
import { useAppStore } from '../stores/app'
import { productAPI } from '../api'
import { getImageUrl } from '../utils/image'
import { useCartStore } from '../stores/cart'
import { useBuyNowStore } from '../stores/buyNow'
import { useUserAuthStore } from '../stores/userAuth'
import { useUserProfileStore } from '../stores/userProfile'
import { debounceAsync } from '../utils/debounce'
import { buildSkuDisplayText, normalizeSkuId } from '../utils/sku'
import { resolveSkuAvailableStock, resolveSkuStockDisplay, type PublicStockDisplay } from '../utils/publicStock'
import { useLocalized, useProductLabels } from './useProduct'
import { toast } from './useToast'

/**
 * 商品详情页的全部业务逻辑（数据加载、SKU/数量、促销/会员/批发定价、库存约束、
 * 加购 / 立即购买、SEO/JSON-LD）。classic 与 vault 模板共用此 composable，
 * 仅各自负责 markup，保证功能严格一致。
 *
 * 视图专属的移动端购买条（IntersectionObserver + DOM ref）仍留在各视图中，
 * 通过 options.onLoaded 在商品加载完成后回调以初始化。
 */
export function useProductDetail(options: { onLoaded?: () => void } = {}) {
  const route = useRoute()
  const router = useRouter()
  const { t } = useI18n()
  const appStore = useAppStore()
  const cartStore = useCartStore()
  const buyNowStore = useBuyNowStore()
  const userAuthStore = useUserAuthStore()
  const userProfileStore = useUserProfileStore()

  const { getLocalizedText, siteCurrency, formatPrice } = useLocalized()
  const {
    getPurchaseTypeLabel, getFulfillmentTypeLabel, getStockBadgeVariant, getStockStatusLabel,
    hasPromotionPrice, getPromotionPriceAmount, getPromotionSaveAmount,
    hasSkuPromotionPrice, getSkuPromotionPriceAmount, getSkuPromotionSaveAmount,
    hasPromotionRules, getPromotionRules, hasWholesalePrices, getWholesalePrices,
    resolveWholesalePriceAmount, resolveMemberPriceAmount,
  } = useProductLabels()

  const formatPromotionRule = (rule: any) => {
    const amount = formatPrice(rule.min_amount, siteCurrency.value)
    const value = rule.type === 'percent' ? String(rule.value) : formatPrice(rule.value, siteCurrency.value)
    const hasMin = Number(rule.min_amount) > 0
    switch (rule.type) {
      case 'percent':
        return hasMin ? t('products.promotionHintPercent', { amount, value }) : t('products.promotionHintPercentNoMin', { value })
      case 'fixed':
        return hasMin ? t('products.promotionHintFixed', { amount, value }) : t('products.promotionHintFixedNoMin', { value })
      case 'special_price':
        return hasMin ? t('products.promotionHintSpecial', { amount, value }) : t('products.promotionHintSpecialNoMin', { value })
      default:
        return rule.name || ''
    }
  }

  const loading = ref(true)
  const product = ref<any>(null)
  const relatedPosts = computed<any[]>(() => product.value?.related_posts || [])
  const formatRelatedPostDate = (dateString: string) => {
    if (!dateString) return ''
    const date = new Date(dateString)
    return date.toLocaleDateString(appStore.locale, { year: 'numeric', month: 'long', day: 'numeric' })
  }
  const currentImage = ref<string>('')
  const selectedSkuId = ref(0)
  const quantity = ref(1)
  const cardCheckEnabled = ref(false)
  const purchaseWarning = ref('')

  const pickCountry = ref('')
  const pickHead = ref<string>('') // 首位挑卡：卡号首位 "3"/"4"/"5"/"6"（与「挑卡种类」模式配合）
  const pickCardTypes = ref<string[]>([])
  const pickStockItems = ref<any[]>([])
  const pickCountries = ref<any[]>([])
  const pickStockLoading = ref(false)

  const pickMode = ref<'random' | 'bin' | 'type' | ''>('')
  const pickBin = ref('')
  const binStockCount = ref<number | null>(null)
  const binStockLoading = ref(false)
  const headStockCount = ref<number | null>(null)
  const headStockLoading = ref(false)
  const countrySearch = ref('')
  const countryDropdownOpen = ref(false)

  const PICK_RANDOM = 'random'

  // 首位挑卡：3头/4头/5头/6头 = 卡号首位 3/4/5/6，与 bot「N头」按钮一致；后端按 1 位 BIN(LIKE 'N%') 匹配。
  const pickHeadOptions = [
    { value: 'random', label: t('productDetail.pickHeadRandom') },
    { value: '3', label: t('productDetail.pickHead3') },
    { value: '4', label: t('productDetail.pickHead4') },
    { value: '5', label: t('productDetail.pickHead5') },
    { value: '6', label: t('productDetail.pickHead6') },
  ]

  // 卡种类合并为 DEBIT（D+PD，提交 D）与 CREDIT（C，提交 C）两类。
  const pickTypeOptions = [
    { value: 'random', label: t('productDetail.pickTypeRandom') },
    { value: 'D', label: t('productDetail.pickTypeDebit') },
    { value: 'C', label: t('productDetail.pickTypeCredit') },
  ]

  // 单选切换：点已选中项取消，点其他项替换。
  const togglePickHead = (value: string) => {
    pickHead.value = pickHead.value === value ? '' : value
  }

  const togglePickType = (value: string) => {
    pickCardTypes.value = pickCardTypes.value[0] === value ? [] : [value]
  }

  const activeSkus = computed(() => {
    const rows = Array.isArray(product.value?.skus) ? product.value.skus : []
    return rows.filter((sku: any) => Boolean(sku?.is_active))
  })

  const selectedSku = computed(() => {
    if (selectedSkuId.value <= 0) return null
    return activeSkus.value.find((sku: any) => normalizeSkuId(sku?.id) === selectedSkuId.value) || null
  })

  // 会员价相关
  const userMemberLevelId = computed(() => Number(userAuthStore.user?.member_level_id || 0))

  const currentMemberDiscountRate = computed(() => {
    if (!userMemberLevelId.value) return 0
    const level = userProfileStore.memberLevels.find((item: any) => Number(item?.id || 0) === userMemberLevelId.value)
    return Number(level?.discount_rate || 0)
  })

  const ensureMemberLevels = () => {
    if (userMemberLevelId.value > 0 && userProfileStore.memberLevels.length === 0) {
      void userProfileStore.loadMemberLevels()
    }
  }

  const getMemberPriceForSku = (skuId: number, basePrice: any): number | null => {
    const price = resolveMemberPriceAmount(product.value, skuId, basePrice, userMemberLevelId.value, currentMemberDiscountRate.value)
    return price === null ? null : Number(price)
  }

  const selectedSkuMemberPrice = computed(() => {
    if (!selectedSku.value) return null
    const skuId = normalizeSkuId(selectedSku.value.id)
    return getMemberPriceForSku(skuId, selectedSku.value.price_amount)
  })

  const hasMemberPrice = computed(() => {
    if (!selectedSkuMemberPrice.value) return false
    const basePrice = Number(selectedSku.value?.price_amount || 0)
    return selectedSkuMemberPrice.value < basePrice
  })

  const selectedSkuWholesaleRules = computed(() => {
    if (!product.value || !selectedSku.value) return []
    return getWholesalePrices(
      product.value,
      normalizeSkuId(selectedSku.value.id),
      selectedSku.value.sku_code,
    )
  })

  const selectedSkuWholesalePrice = computed(() => {
    if (!product.value || !selectedSku.value) return null
    return resolveWholesalePriceAmount(
      product.value,
      selectedSku.value.price_amount,
      quantity.value,
      normalizeSkuId(selectedSku.value.id),
      selectedSku.value.sku_code,
      quantity.value,
    )
  })

  const hasSelectedSkuWholesalePrice = computed(() => {
    if (!selectedSku.value || !selectedSkuWholesalePrice.value) return false
    const comparisonPrice = hasSkuPromotionPrice(selectedSku.value)
      ? Number(getSkuPromotionPriceAmount(selectedSku.value))
      : Number(selectedSku.value.price_amount || 0)
    return Number(selectedSkuWholesalePrice.value) < comparisonPrice
  })

  const selectedSkuWholesaleMemberPrice = computed(() => {
    if (!product.value || !selectedSku.value || !selectedSkuWholesalePrice.value) return null
    const skuId = normalizeSkuId(selectedSku.value.id)
    return getMemberPriceForSku(skuId, selectedSkuWholesalePrice.value)
  })

  const selectedSkuWholesaleFinalIsMember = computed(() => {
    return hasSelectedSkuWholesalePrice.value && selectedSkuWholesaleMemberPrice.value !== null
  })

  const selectedSkuWholesaleFinalPrice = computed(() => {
    if (!hasSelectedSkuWholesalePrice.value || selectedSkuWholesalePrice.value === null) return null
    if (selectedSkuWholesaleMemberPrice.value !== null) return selectedSkuWholesaleMemberPrice.value
    return selectedSkuWholesalePrice.value
  })

  const selectedSkuPromotionPrice = computed(() => {
    if (!selectedSku.value || !hasSkuPromotionPrice(selectedSku.value)) return null
    return getSkuPromotionPriceAmount(selectedSku.value)
  })

  const selectedSkuPromotionMemberPrice = computed(() => {
    if (!product.value || !selectedSku.value || selectedSkuPromotionPrice.value === null) return null
    const skuId = normalizeSkuId(selectedSku.value.id)
    return getMemberPriceForSku(skuId, selectedSkuPromotionPrice.value)
  })

  const selectedSkuPromotionFinalIsMember = computed(() => selectedSkuPromotionMemberPrice.value !== null)

  const selectedSkuPromotionFinalPrice = computed(() => {
    if (selectedSkuPromotionPrice.value === null) return null
    if (selectedSkuPromotionMemberPrice.value !== null) return selectedSkuPromotionMemberPrice.value
    return selectedSkuPromotionPrice.value
  })

  const showSelectedSkuMemberBadge = computed(() => {
    if (!selectedSku.value) return false
    if (hasSelectedSkuWholesalePrice.value) return selectedSkuWholesaleFinalIsMember.value
    if (hasSkuPromotionPrice(selectedSku.value)) return selectedSkuPromotionFinalIsMember.value
    return hasMemberPrice.value
  })

  const formatWholesaleTier = (tier: any) => {
    return t('products.wholesaleTier', {
      count: Number(tier?.min_quantity || 0),
      price: formatPrice(tier?.unit_price, siteCurrency.value),
    })
  }

  const normalizeStockNumber = (value: unknown) => {
    const numberValue = Number(value)
    if (!Number.isFinite(numberValue)) return 0
    return Math.max(Math.floor(numberValue), 0)
  }

  const normalizeManualStockTotal = (value: unknown) => {
    const numberValue = Number(value)
    if (!Number.isFinite(numberValue)) return 0
    const integerValue = Math.floor(numberValue)
    if (integerValue === -1) return -1
    return Math.max(integerValue, 0)
  }

  const normalizeOptionalLimitNumber = (value: unknown) => {
    const numberValue = Number(value)
    if (!Number.isFinite(numberValue)) return null
    const integerValue = Math.floor(numberValue)
    if (integerValue <= 0) return null
    return integerValue
  }

  const shouldEnforceSkuStock = (sku: any) => {
    if (!sku) return false
    if (sku?.stock_quantity_hidden === true || product.value?.stock_quantity_hidden === true) return false
    if (product.value?.fulfillment_type === 'auto') return true
    if (product.value?.fulfillment_type === 'upstream') return true
    if (product.value?.fulfillment_type !== 'manual') return false
    const total = normalizeManualStockTotal(sku?.manual_stock_total)
    if (total === -1) return false
    return true
  }

  const skuAvailableStock = (sku: any) => {
    if (!sku) return 0
    if (!shouldEnforceSkuStock(sku) && !sku?.stock_quantity_hidden) return null
    return resolveSkuAvailableStock(product.value, sku)
  }

  const isSkuPurchasable = (sku: any) => {
    const available = skuAvailableStock(sku)
    if (available === null) return true
    return available > 0
  }

  const formatSkuStockDisplay = (display: PublicStockDisplay) => {
    switch (display.kind) {
      case 'unlimited':
        return t('productDetail.skuStockUnlimited')
      case 'out':
        return t('productDetail.skuStockOut')
      case 'remaining':
        return t('productDetail.skuStockRemaining', { count: display.count })
      case 'low_stock':
        return t('productDetail.skuStockLow')
      case 'hidden':
        return t('productDetail.skuStockHidden')
      case 'range':
        return t('productDetail.skuStockRange', { min: display.min, max: display.max })
      case 'range_plus':
        return t('productDetail.skuStockRangePlus', { min: display.min })
      case 'in_stock':
      default:
        return t('productDetail.skuStockInStock')
    }
  }

  const skuStockText = (sku: any) => {
    const display = resolveSkuStockDisplay(product.value, sku)
    return formatSkuStockDisplay(display)
  }

  const skuStockBadgeClass = (sku: any) => {
    const display = resolveSkuStockDisplay(product.value, sku)
    if (display.kind === 'unlimited' || display.kind === 'hidden') return 'border-slate-200 text-slate-600 dark:border-slate-700 dark:text-slate-300'
    if (display.kind === 'out') return 'border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-700 dark:bg-rose-950/30 dark:text-rose-300'
    if (display.kind === 'low_stock' || (display.kind === 'range' && display.max <= 5)) return 'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-700 dark:bg-amber-950/30 dark:text-amber-300'
    return 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-700 dark:bg-emerald-950/30 dark:text-emerald-300'
  }

  const quantityEffectiveLimit = computed(() => {
    const sku = selectedSku.value
    const available = skuAvailableStock(sku)
    const productLimit = normalizeOptionalLimitNumber(product.value?.max_purchase_quantity)
    let limit: number | null = productLimit
    if (available !== null) {
      limit = limit === null ? available : Math.min(limit, available)
    }
    return limit
  })

  const quantityEffectiveMin = computed(() => {
    const productMin = normalizeOptionalLimitNumber(product.value?.min_purchase_quantity)
    return productMin && productMin > 0 ? productMin : 1
  })

  const handleQuantityInput = (event: Event) => {
    const val = parseInt((event.target as HTMLInputElement).value, 10)
    const minimum = quantityEffectiveMin.value
    if (isNaN(val) || val < minimum) {
      quantity.value = minimum
    } else if (quantityEffectiveLimit.value !== null && val > quantityEffectiveLimit.value) {
      quantity.value = quantityEffectiveLimit.value
    } else {
      quantity.value = val
    }
  }

  const purchaseType = computed(() => product.value?.purchase_type || 'member')
  const requiresLogin = computed(() => purchaseType.value === 'member' && !userAuthStore.isAuthenticated)
  const requiresSKUSelection = computed(() => activeSkus.value.length > 1 && !selectedSku.value)
  const stockBelowMinPurchase = computed(() => {
    const limit = quantityEffectiveLimit.value
    if (limit === null) return false
    return limit < quantityEffectiveMin.value
  })
  // 首位与卡种类都为「随机」时，等价于随机购买（不挑首位也不挑种类），
  // 禁止在“挑卡种类”模式下下单，引导用户切换到“随机购买”模式。
  const isPickBothRandom = computed(() =>
    pickHead.value === PICK_RANDOM && pickCardTypes.value.includes(PICK_RANDOM)
  )

  const pickHeadSelected = computed(() => Boolean(pickHead.value) && pickHead.value !== PICK_RANDOM)
  const pickTypeSelected = computed(() =>
    pickCardTypes.value.length > 0 && pickCardTypes.value[0] !== PICK_RANDOM
  )

  const canPurchase = computed(() => {
    if (!product.value) return false
    if (activeSkus.value.length === 0) return false
    if (product.value.is_sold_out) return false
    if (requiresSKUSelection.value) return false
    if (product.value.stock_status === 'out_of_stock') return false
    if (selectedSku.value && !isSkuPurchasable(selectedSku.value)) return false
    if (stockBelowMinPurchase.value) return false
    if (pickEnabled.value) {
      if (!pickMode.value) return false
      if (pickMode.value === 'bin') {
        if (!/^\d{6}$/.test(pickBin.value)) return false
        if (binStockCount.value !== null && binStockCount.value < quantity.value) return false
      } else {
        if (!pickCountry.value) return false
        if (pickMode.value === 'type') {
          if (isPickBothRandom.value) return false
          if (!pickHeadSelected.value && !pickTypeSelected.value) return false
        }
        if (pickAvailableCount.value < quantity.value) return false
      }
      // 「当前所选」灰框同行动态库存：实时三维计数为 0 时不允许下单（null = 加载中/未查，不阻断，后端发货再校验）。
      if (selectionStockCount.value !== null && selectionStockCount.value < quantity.value) return false
    }
    return true
  })
  const cannotPurchaseReason = computed(() => {
    if (!product.value) return ''
    if (requiresLogin.value) return ''
    if (requiresSKUSelection.value) return t('productDetail.skuRequired')
    if (stockBelowMinPurchase.value) return t('productDetail.stockBelowMinPurchase', { count: quantityEffectiveMin.value })
    if (pickEnabled.value) {
      if (!pickMode.value) return t('productDetail.pickModeRequired')
      if (pickMode.value === 'bin') {
        if (!/^\d{6}$/.test(pickBin.value)) return t('productDetail.pickBinHint')
        if (binStockCount.value !== null && binStockCount.value < quantity.value) return t('productDetail.pickStockInsufficient', { count: binStockCount.value })
      } else {
        if (!pickCountry.value) return t('productDetail.pickCountryRequired')
        if (pickMode.value === 'type') {
          if (isPickBothRandom.value) return t('productDetail.pickBothRandomGuide')
          if (!pickHeadSelected.value && !pickTypeSelected.value) return t('productDetail.pickTypeRequired')
        }
        if (pickAvailableCount.value < quantity.value) return t('productDetail.pickStockInsufficient', { count: pickAvailableCount.value })
      }
      if (selectionStockCount.value !== null && selectionStockCount.value < quantity.value) return t('productDetail.pickStockInsufficient', { count: selectionStockCount.value })
    }
    if (canPurchase.value) return ''
    return t('productDetail.stockUnavailable')
  })
  const categoryName = computed(() => {
    const category = product.value?.category?.name
    return category ? getLocalizedText(category) : ''
  })

  const images = computed(() => {
    if (!product.value?.images) return []
    let imageArray: string[] = []
    if (Array.isArray(product.value.images)) {
      imageArray = product.value.images
    } else if (product.value.images.images && Array.isArray(product.value.images.images)) {
      imageArray = product.value.images.images
    }
    return imageArray.map(img => getImageUrl(img))
  })

  const skuDisplayText = (sku: any) => {
    return buildSkuDisplayText({
      skuCode: sku?.sku_code,
      specValues: sku?.spec_values,
      fallback: t('productDetail.skuFallback'),
      locale: appStore.locale,
    })
  }

  const syncSelectedSku = () => {
    const rows = activeSkus.value
    if (rows.length === 0) {
      selectedSkuId.value = 0
      return
    }
    if (rows.length === 1) {
      selectedSkuId.value = normalizeSkuId(rows[0]?.id)
      return
    }
    if (rows.some((sku: any) => normalizeSkuId(sku?.id) === selectedSkuId.value)) {
      return
    }
    const firstAvailable = rows.find((sku: any) => isSkuPurchasable(sku))
    if (firstAvailable) {
      selectedSkuId.value = normalizeSkuId(firstAvailable?.id)
      return
    }
    selectedSkuId.value = normalizeSkuId(rows[0]?.id)
  }

  const selectedCartQuantity = () => {
    if (!product.value || !selectedSku.value) return 0
    const productId = Number(product.value.id || 0)
    const skuId = normalizeSkuId(selectedSku.value?.id)
    if (productId <= 0 || skuId <= 0) return 0
    const matched = cartStore.items.find((item) => item.productId === productId && normalizeSkuId(item.skuId) === skuId)
    return Number(matched?.quantity || 0)
  }

  const buildItemPayload = (sku: any) => ({
    productId: product.value.id,
    skuId: normalizeSkuId(sku?.id),
    skuCode: String(sku?.sku_code || ''),
    skuSpecValues: (sku?.spec_values && typeof sku.spec_values === 'object') ? sku.spec_values : undefined,
    skuManualStockTotal: normalizeManualStockTotal(sku?.manual_stock_total),
    skuManualStockLocked: normalizeStockNumber(sku?.manual_stock_locked),
    skuManualStockSold: normalizeStockNumber(sku?.manual_stock_sold),
    skuAutoStockAvailable: normalizeStockNumber(sku?.auto_stock_available),
    skuUpstreamStock: normalizeManualStockTotal(sku?.upstream_stock),
    skuStockStatus: String(sku?.stock_status || ''),
    skuStockDisplayMode: String(sku?.stock_display_mode || product.value?.stock_display_mode || ''),
    skuStockDisplay: String(sku?.stock_display || ''),
    skuStockRangeMin: normalizeStockNumber(sku?.stock_range_min) || undefined,
    skuStockRangeMax: normalizeStockNumber(sku?.stock_range_max) || undefined,
    skuStockQuantityHidden: Boolean(sku?.stock_quantity_hidden || product.value?.stock_quantity_hidden),
    skuStockEnforced: shouldEnforceSkuStock(sku),
    slug: product.value.slug,
    title: product.value.title,
    priceAmount: String(sku?.price_amount || product.value.price_amount || '0.00'),
    wholesalePrices: Array.isArray(product.value.wholesale_prices) ? product.value.wholesale_prices : undefined,
    image: images.value[0],
    minPurchaseQuantity: normalizeOptionalLimitNumber(product.value.min_purchase_quantity) ?? undefined,
    maxPurchaseQuantity: normalizeOptionalLimitNumber(product.value.max_purchase_quantity) ?? undefined,
    purchaseType: product.value.purchase_type,
    fulfillmentType: product.value.fulfillment_type,
    manualFormSchema: product.value.manual_form_schema || {},
    paymentChannelIds: Array.isArray(product.value.payment_channel_ids) && product.value.payment_channel_ids.length > 0 ? product.value.payment_channel_ids : undefined,
    cardCheckEnabled: cardCheckEnabled.value,
    cardCheckFee: String(product.value?.card_check_fee || '0'),
    pickSurcharge: pickEnabled.value ? pickUnitSurcharge.value : 0,
    pickCountry: pickEnabled.value ? pickCountry.value : '',
    pickBrands: [], // 首位挑卡取代品牌维度，恒空（后端库存匹配 brand IN ? 仅当非空时生效）
    pickCardTypes: pickEnabled.value ? pickCardTypes.value.filter((t) => t !== PICK_RANDOM) : [],
    // 挑卡种类模式选了首位时提交 1 位 pickBin（后端按 LIKE 'N%' 匹配 + head<N> 加价）；
    // 挑头模式提交 6 位 pickBin。
    pickBin: pickEnabled.value
      ? (pickMode.value === 'type' && pickHead.value && pickHead.value !== PICK_RANDOM ? pickHead.value : pickBin.value)
      : '',
    quantity: quantity.value,
  })

  const addToCart = () => {
    if (!product.value) return
    if (!canPurchase.value) return
    purchaseWarning.value = ''
    if (requiresLogin.value) {
      router.push(`/auth/login?redirect=${encodeURIComponent(route.fullPath)}`)
      return
    }
    const sku = selectedSku.value
    const available = skuAvailableStock(sku)
    const cartQty = selectedCartQuantity()
    const nextQuantity = cartQty + quantity.value
    const productLimit = normalizeOptionalLimitNumber(product.value?.max_purchase_quantity)
    let effectiveLimit: number | null = productLimit
    if (available !== null) {
      effectiveLimit = effectiveLimit === null ? available : Math.min(effectiveLimit, available)
    }
    if (effectiveLimit !== null && nextQuantity > effectiveLimit) {
      if (available !== null && effectiveLimit === available && (productLimit === null || available <= productLimit)) {
        purchaseWarning.value = available > 0
          ? (cartQty > 0
              ? t('productDetail.addCartStockExceededWithCart', { count: available, cartCount: cartQty })
              : t('productDetail.addCartStockExceeded', { count: available }))
          : t('productDetail.stockUnavailable')
        return
      }
      purchaseWarning.value = cartQty > 0
        ? t('productDetail.addCartLimitExceededWithCart', { count: effectiveLimit, cartCount: cartQty })
        : t('productDetail.addCartLimitExceeded', { count: effectiveLimit })
      return
    }
    cartStore.addItem(buildItemPayload(sku), quantity.value)
    toast.success(t('toast.addedToCart'))
  }

  const buyNow = () => {
    purchaseWarning.value = ''
    if (!canPurchase.value) return
    if (!product.value) return
    if (requiresLogin.value) {
      router.push(`/auth/login?redirect=${encodeURIComponent(route.fullPath)}`)
      return
    }

    const sku = selectedSku.value
    const available = skuAvailableStock(sku)
    const productLimit = normalizeOptionalLimitNumber(product.value?.max_purchase_quantity)
    let limit: number | null = productLimit
    if (available !== null) {
      limit = limit === null ? available : Math.min(limit, available)
    }
    if (limit !== null && quantity.value > limit) {
      purchaseWarning.value = available !== null && limit === available
        ? (available > 0 ? t('productDetail.addCartStockExceeded', { count: available }) : t('productDetail.stockUnavailable'))
        : t('productDetail.addCartLimitExceeded', { count: limit })
      return
    }

    buyNowStore.setItem(buildItemPayload(sku))
    router.push('/checkout?mode=buynow')
  }

  // 移动端购买条价格展示
  const mobileBarShowMemberPrice = computed(() => {
    if (!selectedSku.value) return false
    if (hasSelectedSkuWholesalePrice.value) return selectedSkuWholesaleFinalIsMember.value
    if (hasSkuPromotionPrice(selectedSku.value)) return selectedSkuPromotionFinalIsMember.value
    return hasMemberPrice.value
  })
  const mobileBarMemberPriceDisplay = computed(() => {
    if (hasSelectedSkuWholesalePrice.value && selectedSkuWholesaleFinalIsMember.value) {
      return formatPrice(selectedSkuWholesaleFinalPrice.value, siteCurrency.value)
    }
    if (selectedSku.value && hasSkuPromotionPrice(selectedSku.value) && selectedSkuPromotionFinalIsMember.value) {
      return formatPrice(selectedSkuPromotionFinalPrice.value, siteCurrency.value)
    }
    if (!selectedSkuMemberPrice.value) return ''
    return formatPrice(selectedSkuMemberPrice.value, siteCurrency.value)
  })
  const mobileBarShowSkuPromotionPrice = computed(() => {
    if (mobileBarShowMemberPrice.value) return false
    if (hasSelectedSkuWholesalePrice.value) return false
    return !!selectedSku.value && hasSkuPromotionPrice(selectedSku.value)
  })
  const mobileBarSkuPromotionPriceDisplay = computed(() => {
    if (!selectedSku.value) return ''
    return formatPrice(getSkuPromotionPriceAmount(selectedSku.value), siteCurrency.value)
  })
  const mobileBarShowSkuPrice = computed(() => {
    if (mobileBarShowMemberPrice.value || mobileBarShowSkuPromotionPrice.value) return false
    return !!selectedSku.value
  })
  const mobileBarSkuPriceDisplay = computed(() => {
    if (!selectedSku.value) return ''
    return formatPrice(selectedSku.value.price_amount, siteCurrency.value)
  })
  const mobileBarShowProductPromotionPrice = computed(() => {
    if (selectedSku.value) return false
    return product.value ? hasPromotionPrice(product.value) : false
  })
  const mobileBarProductPromotionPriceDisplay = computed(() => {
    if (!product.value) return ''
    return formatPrice(getPromotionPriceAmount(product.value), siteCurrency.value)
  })
  const mobileBarProductPriceDisplay = computed(() => {
    if (!product.value) return ''
    return formatPrice(product.value.price_amount, siteCurrency.value)
  })

  const goLogin = () => {
    router.push(`/auth/login?redirect=${encodeURIComponent(route.fullPath)}`)
  }

  const loadProduct = async () => {
    loading.value = true
    try {
      const slug = route.params.slug as string
      const response = await productAPI.detail(slug)
      product.value = response.data.data || null
      resetPickSelection()
      await loadPickStock()
      if (images.value.length > 0) {
        currentImage.value = images.value[0] || ''
      }
      syncSelectedSku()
      await nextTick()
      options.onLoaded?.()
    } catch (error) {
      console.error('Failed to load product:', error)
      product.value = null
      selectedSkuId.value = 0
    } finally {
      loading.value = false
    }
  }

  const debouncedLoadProduct = debounceAsync(loadProduct, 300)

  const canonicalUrl = computed(() => {
    if (!product.value?.slug) return ''
    const fromConfig = String(appStore.config?.brand?.site_url || '').trim().replace(/\/+$/, '')
    const base = fromConfig || window.location.origin.replace(/\/+$/, '')
    return `${base}/products/${product.value.slug}`
  })

  useHead({
    title: () => product.value ? getLocalizedText(product.value.title) : '',
    link: () => {
      if (!canonicalUrl.value) return []
      return [{ rel: 'canonical', href: canonicalUrl.value }]
    },
    meta: () => {
      if (!product.value) return []
      const seoMeta = product.value.seo_meta || {}
      const seoKeywords = getLocalizedText(seoMeta.keywords) || (typeof seoMeta.keywords === 'string' ? seoMeta.keywords : '')
      const seoDescription = getLocalizedText(seoMeta.description) || (typeof seoMeta.description === 'string' ? seoMeta.description : '')
      const tags = []

      if (seoKeywords) tags.push({ name: 'keywords', content: seoKeywords })
      if (seoDescription) tags.push({ name: 'description', content: seoDescription })

      tags.push({ property: 'og:type', content: 'product' })
      if (product.value.title) {
        tags.push({ property: 'og:title', content: getLocalizedText(product.value.title) })
      }
      if (seoDescription) {
        tags.push({ property: 'og:description', content: seoDescription })
      }
      if (images.value && images.value.length > 0) {
        tags.push({ property: 'og:image', content: images.value[0] })
      }
      if (canonicalUrl.value) {
        tags.push({ property: 'og:url', content: canonicalUrl.value })
      }

      tags.push({ name: 'twitter:card', content: 'summary_large_image' })
      if (product.value.title) {
        tags.push({ name: 'twitter:title', content: getLocalizedText(product.value.title) })
      }
      if (seoDescription) {
        tags.push({ name: 'twitter:description', content: seoDescription })
      }
      if (images.value && images.value.length > 0) {
        tags.push({ name: 'twitter:image', content: images.value[0] })
      }

      return tags
    },
    script: () => {
      if (!product.value) return []
      const title = getLocalizedText(product.value.title)
      const seoMeta = product.value.seo_meta || {}
      const description = getLocalizedText(seoMeta.description) || (typeof seoMeta.description === 'string' ? seoMeta.description : '')
      const priceAmount = product.value.price_amount || '0'
      const currency = siteCurrency.value || 'CNY'

      const jsonLd: Record<string, any> = {
        '@context': 'https://schema.org',
        '@type': 'Product',
        name: title,
        url: canonicalUrl.value || window.location.href,
        offers: {
          '@type': 'Offer',
          price: priceAmount,
          priceCurrency: currency,
          availability: product.value.stock_status === 'out_of_stock'
            ? 'https://schema.org/OutOfStock'
            : 'https://schema.org/InStock',
        },
      }
      if (description) jsonLd.description = description
      if (images.value.length > 0) jsonLd.image = images.value
      if (product.value.category?.name) {
        jsonLd.category = getLocalizedText(product.value.category.name)
      }

      return [{
        type: 'application/ld+json',
        innerHTML: JSON.stringify(jsonLd),
      }]
    },
  })

  onMounted(() => {
    loadProduct()
    ensureMemberLevels()
  })

  watch(userMemberLevelId, () => {
    ensureMemberLevels()
  })

  watch(
    () => selectedSkuId.value,
    () => {
      purchaseWarning.value = ''
      quantity.value = quantityEffectiveMin.value
    }
  )

  watch(quantityEffectiveMin, (minimum) => {
    if (minimum > quantity.value) {
      quantity.value = minimum
    }
  })

  watch(quantityEffectiveLimit, (limit) => {
    if (limit !== null && quantity.value > limit) {
      quantity.value = Math.max(quantityEffectiveMin.value, limit)
    }
  })

  const cardCheckFeeAmount = computed<number>(() => Number(product.value?.card_check_fee || 0))
  const cardCheckPlainPrice = computed<number>(() => {
    const sku = selectedSku.value
    const raw = sku ? sku.price_amount : product.value?.price_amount
    return Number(raw || 0)
  })
  const cardCheckCheckedPrice = computed<number>(() => cardCheckPlainPrice.value + cardCheckFeeAmount.value)

  const pickEnabled = computed(() => Boolean(product.value?.pick_enabled))

  const loadPickStock = async () => {
    if (!product.value?.slug || !pickEnabled.value) {
      pickStockItems.value = []
      pickCountries.value = []
      return
    }
    pickStockLoading.value = true
    try {
      const response = await productAPI.pickStock(product.value.slug)
      pickStockItems.value = Array.isArray(response.data.data?.items) ? response.data.data.items : []
      pickCountries.value = Array.isArray(response.data.data?.countries) ? response.data.data.countries : []
    } catch {
      pickStockItems.value = []
      pickCountries.value = []
    } finally {
      pickStockLoading.value = false
    }
  }

  const availableCountries = computed(() => {
    if (!pickEnabled.value) return []
    const available = new Set<string>()
    for (const item of pickStockItems.value) {
      if (Number(item.total) > 0 && item.country) available.add(String(item.country))
    }
    return pickCountries.value.filter((c) => available.has(String(c.code)))
  })

  const pickAvailableCount = computed(() => {
    if (!pickCountry.value) return 0
    const skuId = normalizeSkuId(selectedSku.value?.id)
    // 「随机」视为不限：跳过该维度筛选
    const typeFilter = pickCardTypes.value.filter((t) => t !== PICK_RANDOM)
    // D（含预付）是超集：匹配 D 与纯 D（PD）。
    // 首位维度不在此过滤——首位库存单独展示（pickHeadAvailable，全商品不分国家，与 bot 一致）。
    const typeMatches = (cardType: string) => {
      if (typeFilter.length === 0) return true
      return typeFilter.some((t) => t === cardType || (t === 'D' && cardType === 'PD'))
    }
    let total = 0
    for (const item of pickStockItems.value) {
      if (String(item.country || '') !== pickCountry.value) continue
      if (skuId > 0 && normalizeSkuId(item.sku_id) !== skuId) continue
      if (!typeMatches(String(item.card_type || ''))) continue
      total += Number(item.total || 0)
    }
    return total
  })

  const pickUnitSurcharge = computed<number>(() => {
    const prices = product.value?.pick_prices || {}
    const maxBy = (keys: string[]) => {
      let max = 0
      for (const key of keys) {
        const value = Number(prices[key] || 0)
        if (Number.isFinite(value) && value > max) max = value
      }
      return max
    }
    if (pickMode.value === 'bin') {
      // 挑头（BIN）模式：加价独立配置在 pick_prices["bin"]。
      return Number((maxBy(['bin']) + 0).toFixed(2))
    }
    // 挑卡种类模式：首位维度取代品牌维度，加价取 pick_prices["head<N>"]；种类仍取 D/C。
    const headKey = pickHead.value && pickHead.value !== PICK_RANDOM ? 'head' + pickHead.value : null
    const headSurcharge = headKey ? maxBy([headKey]) : 0
    return Number((headSurcharge + maxBy(pickCardTypes.value)).toFixed(2))
  })

  const pickUnitPrice = computed<number>(() => cardCheckPlainPrice.value + pickUnitSurcharge.value)

  // 最终单价（含挑卡加价）：测活区两档价格统一在此基础上叠加测活费，
  // 避免页面上出现“基础价 / 测活价 / 挑卡单价”多处割裂的价格。
  const finalPlainPrice = computed<number>(() => cardCheckPlainPrice.value + pickUnitSurcharge.value)
  const finalCheckedPrice = computed<number>(() => finalPlainPrice.value + cardCheckFeeAmount.value)

  const pickSelectionSummary = computed(() => {
    if (!pickCountry.value && !pickBin.value && !pickHead.value) return ''
    const parts: string[] = []
    if (pickBin.value) {
      parts.push(`BIN ${pickBin.value}`)
    } else {
      const country = availableCountries.value.find((c) => String(c.code) === pickCountry.value)
      parts.push(country ? `${country.name} ${country.code}` : pickCountry.value)
      if (pickHead.value && pickHead.value !== PICK_RANDOM) {
        const opt = pickHeadOptions.find((o) => o.value === pickHead.value)
        parts.push(opt ? opt.label : `${pickHead.value}头`)
      }
      if (pickCardTypes.value.length) {
        const labels = pickCardTypes.value.map((ty) => pickTypeOptions.find((o) => o.value === ty)?.label || ty)
        parts.push(labels.join('、'))
      }
    }
    return parts.join(' · ')
  })

  const selectPickMode = (mode: 'random' | 'bin' | 'type') => {
    if (pickMode.value === mode) return
    pickMode.value = mode
    pickCountry.value = ''
    pickHead.value = ''
    pickCardTypes.value = []
    pickBin.value = ''
    binStockCount.value = null
    headStockCount.value = null
    selectionStockCount.value = null
    selectionStockLoading.value = false
    if (selectionStockTimer) { clearTimeout(selectionStockTimer); selectionStockTimer = null }
    countrySearch.value = ''
  }

  const selectCountry = (code: string) => {
    pickCountry.value = code
    countrySearch.value = ''
    countryDropdownOpen.value = false
  }

  const onCountryBlur = () => {
    setTimeout(() => { countryDropdownOpen.value = false }, 150)
  }

  const filteredCountries = computed(() => {
    if (!countrySearch.value.trim()) return availableCountries.value
    const q = countrySearch.value.trim().toLowerCase()
    return availableCountries.value.filter((c: any) =>
      String(c.code).toLowerCase().includes(q) || String(c.name).toLowerCase().includes(q),
    )
  })

  const selectedCountryName = computed(() => {
    const c = availableCountries.value.find((c: any) => String(c.code) === pickCountry.value)
    return c ? `${c.name} ${c.code}` : ''
  })

  let binDebounceTimer: ReturnType<typeof setTimeout> | null = null
  const checkBinStock = async () => {
    if (binDebounceTimer) clearTimeout(binDebounceTimer)
    if (!pickEnabled.value || pickBin.value.length !== 6 || !/^\d{6}$/.test(pickBin.value)) {
      binStockCount.value = null
      return
    }
    binDebounceTimer = setTimeout(async () => {
      binStockLoading.value = true
      try {
        const response = await productAPI.pickStock(product.value.slug, pickBin.value)
        binStockCount.value = Number(response.data.data?.bin_total ?? 0)
      } catch {
        binStockCount.value = 0
      } finally {
        binStockLoading.value = false
      }
    }, 400)
  }

  watch(pickBin, () => {
    if (pickBin.value && !/^\d*$/.test(pickBin.value)) {
      pickBin.value = pickBin.value.replace(/\D/g, '').slice(0, 6)
    }
    if (pickMode.value === 'bin') checkBinStock()
  })

  // 首位挑卡库存：选了具体首位（1 位数字）时，复用 pick-stock?bin=<首位> 取全商品该首位库存
  // （不分国家，与 bot headStock 行为一致）；实际下单再由后端按首位+国家 LIKE 匹配精确发货。
  let headDebounceTimer: ReturnType<typeof setTimeout> | null = null
  const checkHeadStock = async () => {
    if (headDebounceTimer) clearTimeout(headDebounceTimer)
    if (!pickEnabled.value || !pickHead.value || pickHead.value === PICK_RANDOM || pickHead.value.length !== 1) {
      headStockCount.value = null
      return
    }
    headDebounceTimer = setTimeout(async () => {
      headStockLoading.value = true
      try {
        const response = await productAPI.pickStock(product.value.slug, pickHead.value)
        headStockCount.value = Number(response.data.data?.bin_total ?? 0)
      } catch {
        headStockCount.value = 0
      } finally {
        headStockLoading.value = false
      }
    }, 300)
  }

  watch(pickHead, () => {
    if (pickMode.value === 'type') checkHeadStock()
  })

  // 「当前所选」灰框同行动态库存：随国家/首位/种类选择逐步筛选，实时调 pick-count 端点。
  // 与后端发货 buildPickQuery 逻辑一致——所见即所得。未达可查条件时不显示。
  const selectionStockCount = ref<number | null>(null)
  const selectionStockLoading = ref(false)
  let selectionStockTimer: ReturnType<typeof setTimeout> | null = null

  const buildPickCountParams = (): { country?: string; bin?: string; card_type?: string } | null => {
    if (!pickEnabled.value || !product.value?.slug) return null
    if (pickMode.value === 'bin') {
      // 挑头模式：6 位 BIN 才查；无国家。
      if (!/^\d{6}$/.test(pickBin.value)) return null
      return { bin: pickBin.value }
    }
    // 随机 / 挑卡种类模式：均需先选国家，否则不显示库存。
    if (!pickCountry.value) return null
    const params: { country?: string; bin?: string; card_type?: string } = { country: pickCountry.value }
    if (pickMode.value === 'type') {
      // 首位：1 位具体数字（非 random）。
      if (pickHead.value && pickHead.value !== PICK_RANDOM && pickHead.value.length === 1) {
        params.bin = pickHead.value
      }
      // 种类：去 random，逗号拼接（后端按需展开 D→D+PD）。
      const types = pickCardTypes.value.filter((t) => t !== PICK_RANDOM)
      if (types.length > 0) params.card_type = types.join(',')
    }
    return params
  }

  const refreshSelectionStock = async () => {
    if (selectionStockTimer) clearTimeout(selectionStockTimer)
    const params = buildPickCountParams()
    if (params === null) {
      selectionStockCount.value = null
      selectionStockLoading.value = false
      return
    }
    selectionStockTimer = setTimeout(async () => {
      selectionStockLoading.value = true
      try {
        const response = await productAPI.pickCount(product.value.slug, params)
        selectionStockCount.value = Number(response.data.data?.count ?? 0)
      } catch {
        selectionStockCount.value = null
      } finally {
        selectionStockLoading.value = false
      }
    }, 400)
  }

  watch(
    [pickMode, pickCountry, pickHead, pickCardTypes, pickBin],
    () => { refreshSelectionStock() },
  )

  const resetPickSelection = () => {
    pickMode.value = ''
    pickCountry.value = ''
    pickHead.value = ''
    pickCardTypes.value = []
    pickBin.value = ''
    binStockCount.value = null
    headStockCount.value = null
    selectionStockCount.value = null
    selectionStockLoading.value = false
    if (selectionStockTimer) { clearTimeout(selectionStockTimer); selectionStockTimer = null }
    countrySearch.value = ''
    countryDropdownOpen.value = false
  }

  onUnmounted(() => {
    debouncedLoadProduct.cancel()
  })

  return {
    // 国际化/格式化（模板需要）
    getLocalizedText, siteCurrency, formatPrice,
    getPurchaseTypeLabel, getFulfillmentTypeLabel, getStockBadgeVariant, getStockStatusLabel,
    hasPromotionPrice, getPromotionPriceAmount, getPromotionSaveAmount,
    hasSkuPromotionPrice, getSkuPromotionPriceAmount, getSkuPromotionSaveAmount,
    hasPromotionRules, getPromotionRules, hasWholesalePrices, getWholesalePrices,
    formatPromotionRule, formatWholesaleTier, formatRelatedPostDate,
    normalizeSkuId,
    // 状态
    loading, product, relatedPosts, currentImage, selectedSkuId, quantity, purchaseWarning,
    activeSkus, selectedSku,
    // 测活
    cardCheckEnabled, cardCheckFeeAmount, cardCheckPlainPrice, cardCheckCheckedPrice,
    finalPlainPrice, finalCheckedPrice,
    // 挑卡
    pickEnabled, pickCountry, pickHead, pickCardTypes, pickStockLoading,
    pickHeadOptions, pickTypeOptions, togglePickHead, togglePickType, pickSelectionSummary,
    isPickBothRandom,
    pickMode, pickBin, binStockCount, binStockLoading, headStockCount, headStockLoading, selectPickMode,
    countrySearch, countryDropdownOpen, selectCountry, onCountryBlur, filteredCountries, selectedCountryName,
    availableCountries, pickAvailableCount, pickUnitSurcharge, pickUnitPrice,
    selectionStockCount, selectionStockLoading,
    resetPickSelection,
    // 价格计算
    selectedSkuMemberPrice, hasMemberPrice,
    hasSelectedSkuWholesalePrice, selectedSkuWholesaleFinalIsMember, selectedSkuWholesaleFinalPrice,
    selectedSkuWholesaleRules,
    selectedSkuPromotionPrice, selectedSkuPromotionFinalIsMember, selectedSkuPromotionFinalPrice,
    showSelectedSkuMemberBadge,
    // SKU / 库存 / 数量
    isSkuPurchasable, skuDisplayText, skuStockText, skuStockBadgeClass,
    quantityEffectiveLimit, quantityEffectiveMin, handleQuantityInput,
    // 购买能力
    purchaseType, requiresLogin, requiresSKUSelection, canPurchase, cannotPurchaseReason,
    categoryName, images,
    // 动作
    addToCart, buyNow, goLogin, loadProduct,
    // 移动端购买条
    mobileBarShowMemberPrice, mobileBarMemberPriceDisplay,
    mobileBarShowSkuPromotionPrice, mobileBarSkuPromotionPriceDisplay,
    mobileBarShowSkuPrice, mobileBarSkuPriceDisplay,
    mobileBarShowProductPromotionPrice, mobileBarProductPromotionPriceDisplay, mobileBarProductPriceDisplay,
  }
}
