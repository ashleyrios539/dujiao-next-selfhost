<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { notifyError, notifySuccess } from '@/utils/notify'

const { t } = useI18n()

const loading = ref(false)
const submitting = ref(false)

const form = reactive({
  enabled: false,
  kami: '',
  interface: '',
  country: '美国',
  buffer: 5,
  timeout_seconds: 60,
  poll_interval_millis: 2000,
})

const clamp = (value: unknown, min: number, max: number, fallback: number) => {
  const parsed = Number(value)
  if (Number.isNaN(parsed)) return fallback
  if (parsed < min) return min
  if (parsed > max) return max
  return parsed
}

const toText = (value: unknown): string => (typeof value === 'string' ? value : '')

const loadConfig = async () => {
  loading.value = true
  try {
    const res = await adminAPI.getSettings({ key: 'card_check_config' })
    const data = res.data?.data as Record<string, unknown> | undefined
    if (data) {
      form.enabled = data.enabled === true
      form.kami = toText(data.kami)
      form.interface = toText(data.interface)
      form.country = toText(data.country) || '美国'
      form.buffer = clamp(data.buffer, 0, 100, 5)
      form.timeout_seconds = clamp(data.timeout_seconds, 10, 300, 60)
      form.poll_interval_millis = clamp(data.poll_interval_millis, 500, 10000, 2000)
    }
  } catch {
    // ignore load error, use defaults
  } finally {
    loading.value = false
  }
}

const save = async () => {
  submitting.value = true
  try {
    const normalized = {
      enabled: form.enabled,
      kami: form.kami.trim(),
      interface: form.interface.trim(),
      country: form.country.trim() || '美国',
      buffer: clamp(form.buffer, 0, 100, 5),
      timeout_seconds: clamp(form.timeout_seconds, 10, 300, 60),
      poll_interval_millis: clamp(form.poll_interval_millis, 500, 10000, 2000),
    }
    form.buffer = normalized.buffer
    form.timeout_seconds = normalized.timeout_seconds
    form.poll_interval_millis = normalized.poll_interval_millis
    await adminAPI.updateSettings({
      key: 'card_check_config',
      value: normalized,
    })
    notifySuccess(t('admin.settings.alerts.saveSuccess'))
  } catch (err) {
    const known = err as Error & { __notified?: boolean }
    if (!known?.__notified) {
      notifyError(known?.message || t('admin.settings.alerts.saveFailed'))
    }
  } finally {
    submitting.value = false
  }
}

defineExpose({ save, submitting })

onMounted(() => {
  loadConfig()
})
</script>

<template>
  <div class="space-y-6">
    <div class="rounded-lg border p-6">
      <h2 class="text-lg font-semibold">{{ t('admin.settings.cardCheck.title') }}</h2>
      <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.settings.cardCheck.subtitle') }}</p>
    </div>

    <div class="rounded-lg border p-6 space-y-4">
      <div>
        <h3 class="text-sm font-semibold">{{ t('admin.settings.cardCheck.enabled.title') }}</h3>
        <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.settings.cardCheck.enabled.subtitle') }}</p>
      </div>
      <div class="flex items-center gap-3">
        <Switch v-model="form.enabled" />
        <Label class="text-sm font-medium">{{ t('admin.settings.cardCheck.enabled.label') }}</Label>
      </div>
      <p class="text-xs text-muted-foreground">{{ t('admin.settings.cardCheck.enabled.hint') }}</p>
    </div>

    <div class="rounded-lg border p-6 space-y-4">
      <div>
        <h3 class="text-sm font-semibold">{{ t('admin.settings.cardCheck.kami.title') }}</h3>
        <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.settings.cardCheck.kami.subtitle') }}</p>
      </div>
      <div class="space-y-1">
        <label class="text-xs font-medium text-muted-foreground">{{ t('admin.settings.cardCheck.kami.label') }}</label>
        <Input v-model="form.kami" type="password" autocomplete="off" :placeholder="t('admin.settings.cardCheck.kami.placeholder')" />
        <p class="text-xs text-muted-foreground">{{ t('admin.settings.cardCheck.kami.hint') }}</p>
      </div>
    </div>

    <div class="rounded-lg border p-6 space-y-4">
      <div>
        <h3 class="text-sm font-semibold">{{ t('admin.settings.cardCheck.interface.title') }}</h3>
        <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.settings.cardCheck.interface.subtitle') }}</p>
      </div>
      <div class="space-y-1">
        <label class="text-xs font-medium text-muted-foreground">{{ t('admin.settings.cardCheck.interface.label') }}</label>
        <Input v-model="form.interface" :placeholder="t('admin.settings.cardCheck.interface.placeholder')" />
        <p class="text-xs text-muted-foreground">{{ t('admin.settings.cardCheck.interface.hint') }}</p>
      </div>
    </div>

    <div class="rounded-lg border p-6 space-y-4">
      <div>
        <h3 class="text-sm font-semibold">{{ t('admin.settings.cardCheck.country.title') }}</h3>
        <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.settings.cardCheck.country.subtitle') }}</p>
      </div>
      <div class="space-y-1">
        <label class="text-xs font-medium text-muted-foreground">{{ t('admin.settings.cardCheck.country.label') }}</label>
        <Input v-model="form.country" :placeholder="t('admin.settings.cardCheck.country.placeholder')" />
        <p class="text-xs text-muted-foreground">{{ t('admin.settings.cardCheck.country.hint') }}</p>
      </div>
    </div>

    <div class="rounded-lg border p-6 space-y-4">
      <div>
        <h3 class="text-sm font-semibold">{{ t('admin.settings.cardCheck.buffer.title') }}</h3>
        <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.settings.cardCheck.buffer.subtitle') }}</p>
      </div>
      <div class="space-y-1">
        <label class="text-xs font-medium text-muted-foreground">{{ t('admin.settings.cardCheck.buffer.label') }}</label>
        <Input v-model.number="form.buffer" type="number" min="0" max="100" />
        <p class="text-xs text-muted-foreground">{{ t('admin.settings.cardCheck.buffer.hint') }}</p>
      </div>
    </div>

    <div class="rounded-lg border p-6 space-y-4">
      <div>
        <h3 class="text-sm font-semibold">{{ t('admin.settings.cardCheck.timeout.title') }}</h3>
        <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.settings.cardCheck.timeout.subtitle') }}</p>
      </div>
      <div class="space-y-1">
        <label class="text-xs font-medium text-muted-foreground">{{ t('admin.settings.cardCheck.timeout.label') }}</label>
        <Input v-model.number="form.timeout_seconds" type="number" min="10" max="300" />
        <p class="text-xs text-muted-foreground">{{ t('admin.settings.cardCheck.timeout.hint') }}</p>
      </div>
    </div>

    <div class="rounded-lg border p-6 space-y-4">
      <div>
        <h3 class="text-sm font-semibold">{{ t('admin.settings.cardCheck.poll.title') }}</h3>
        <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.settings.cardCheck.poll.subtitle') }}</p>
      </div>
      <div class="space-y-1">
        <label class="text-xs font-medium text-muted-foreground">{{ t('admin.settings.cardCheck.poll.label') }}</label>
        <Input v-model.number="form.poll_interval_millis" type="number" min="500" max="10000" step="100" />
        <p class="text-xs text-muted-foreground">{{ t('admin.settings.cardCheck.poll.hint') }}</p>
      </div>
    </div>
  </div>
</template>
