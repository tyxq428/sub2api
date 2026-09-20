<template>
  <div class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-700">
    <div class="flex items-start justify-between gap-3">
      <div>
        <div class="text-sm font-medium text-gray-900 dark:text-gray-100">
          {{ t('admin.accounts.openai.codexR2Title') }}
        </div>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.openai.codexR2Desc') }}
        </p>
      </div>
      <button type="button" class="btn-secondary px-2 py-1 text-xs" :disabled="loading" @click="$emit('refresh')">
        {{ t('common.refresh') }}
      </button>
    </div>

    <div v-if="loading" class="mt-3 text-xs text-gray-500 dark:text-gray-400">
      {{ t('common.loading') }}
    </div>
    <div v-else-if="error" class="mt-3 rounded bg-red-50 p-2 text-xs text-red-700 dark:bg-red-950/30 dark:text-red-300">
      {{ error }}
    </div>
    <template v-else-if="state">
      <div class="mt-3 grid grid-cols-2 gap-2 text-xs md:grid-cols-4">
        <Metric :label="t('admin.accounts.openai.codexR2EffectiveMode')" :value="state.effective_mode" />
        <Metric :label="t('admin.accounts.openai.codexR2Reason')" :value="state.reason" />
        <Metric :label="t('admin.accounts.openai.codexR2ClientUA')" :value="state.client_ua_mode" />
        <Metric :label="t('admin.accounts.openai.codexR2Profile')" :value="state.reference_profile" />
        <Metric :label="t('admin.accounts.openai.codexR2Fingerprint')" :value="state.fingerprint_mode" />
        <Metric :label="t('admin.accounts.openai.codexR2BindingsActive')" :value="String(state.bindings.active)" />
        <Metric :label="t('admin.accounts.openai.codexR2BindingsDraining')" :value="String(state.bindings.draining)" />
        <Metric :label="t('admin.accounts.openai.codexR2Coverage')" :value="coverageText" />
      </div>

      <div class="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-500 dark:text-gray-400">
        <span>{{ t('admin.accounts.openai.codexR2Observed') }}: {{ totals.total }}</span>
        <span>{{ t('admin.accounts.openai.codexR2Conflicts') }}: {{ totals.conflicts }}</span>
        <span>{{ t('admin.accounts.openai.codexR2Unknown') }}: {{ totals.unknown }}</span>
        <span>{{ t('admin.accounts.openai.codexR2Shadow') }}: {{ state.shadow_telemetry ? 'on' : 'off' }}</span>
      </div>

      <div
        v-if="state.effective_mode === 'enforce' && !state.mapping_key_configured"
        class="mt-3 rounded bg-amber-50 p-2 text-xs text-amber-800 dark:bg-amber-950/30 dark:text-amber-200"
      >
        {{ t('admin.accounts.openai.codexR2MappingKeyMissing') }}
      </div>

      <div v-if="state.bindings.active > 0" class="mt-3 flex justify-end">
        <button type="button" class="btn-secondary px-3 py-1.5 text-xs" :disabled="draining" @click="$emit('drain')">
          {{ draining ? t('common.processing') : t('admin.accounts.openai.codexR2Drain') }}
        </button>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CodexR2AccountState } from '@/types'

const props = defineProps<{
  state: CodexR2AccountState | null
  loading: boolean
  error: string
  draining: boolean
}>()

defineEmits<{
  refresh: []
  drain: []
}>()

const { t } = useI18n()

const totals = computed(() =>
  (props.state?.shadow ?? []).reduce(
    (acc, row) => {
      acc.total += row.total_events
      acc.complete += row.complete_events
      acc.conflicts += row.conflict_events
      acc.unknown += row.unknown_events
      return acc
    },
    { total: 0, complete: 0, conflicts: 0, unknown: 0 }
  )
)

const coverageText = computed(() => {
  if (totals.value.total <= 0) return '—'
  return `${((totals.value.complete / totals.value.total) * 100).toFixed(1)}%`
})
</script>

<script lang="ts">
import { defineComponent, h } from 'vue'

const Metric = defineComponent({
  props: {
    label: { type: String, required: true },
    value: { type: String, required: true }
  },
  setup(props) {
    return () =>
      h('div', { class: 'min-w-0 rounded bg-white p-2 dark:bg-dark-800' }, [
        h('div', { class: 'truncate text-gray-500 dark:text-gray-400' }, props.label),
        h('div', { class: 'mt-1 break-all font-medium text-gray-900 dark:text-gray-100' }, props.value)
      ])
  }
})

export default { components: { Metric } }
</script>
