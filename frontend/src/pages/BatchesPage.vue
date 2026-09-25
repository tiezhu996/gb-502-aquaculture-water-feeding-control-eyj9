<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Box, CircleCheck, Link, Plus, Warning } from '@element-plus/icons-vue'
import { feedBatchApi } from '@/api/feedBatches'
import MetricCard from '@/components/common/MetricCard.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import { useAuth } from '@/hooks/useAuth'
import { useQueryParams } from '@/hooks/useQueryParams'
import type { FeedBatch, FeedBatchInput, FeedConsumption, PageQuery } from '@/types/models'
import { errorMessage } from '@/utils/errors'
import { formatDate, formatDateTime, formatNumber, toISO, toLocalDateInput } from '@/utils/format'

const { canOperate } = useAuth()
const { params } = useQueryParams({ search: '', feedType: '', enabled: '', page: 1 })
const batches = ref<FeedBatch[]>([])
const total = ref(0)
const loading = ref(false)
const saving = ref(false)
const editorOpen = ref(false)
const editingId = ref<number | null>(null)
const target = ref<FeedBatch | null>(null)
const consumptions = ref<FeedConsumption[]>([])
const consumptionOpen = ref(false)
const consumptionLoading = ref(false)

const emptyForm = (): FeedBatchInput => ({
  batchNumber: '', feedType: '', inboundAmountKg: 0,
  expiryDate: toLocalDateInput(new Date(Date.now() + 30 * 86400000)), enabled: true, notes: '',
})
const form = reactive<FeedBatchInput>(emptyForm())

function expiryEnd(batch: FeedBatch): number {
  const end = new Date(batch.expiryDate)
  end.setUTCHours(23, 59, 59, 999)
  return end.getTime()
}

function isExpired(batch: FeedBatch): boolean {
  return Date.now() > expiryEnd(batch)
}

function isNearExpiry(batch: FeedBatch): boolean {
  const days = (expiryEnd(batch) - Date.now()) / 86400000
  return days >= 0 && days <= 7
}

const enabledCount = computed(() => batches.value.filter((item) => item.enabled && !isExpired(item)).length)
const nearExpiryCount = computed(() => batches.value.filter((item) => item.enabled && isNearExpiry(item)).length)
const expiredCount = computed(() => batches.value.filter((item) => isExpired(item)).length)
const zeroStockCount = computed(() => batches.value.filter((item) => item.enabled && !isExpired(item) && item.remainingKg <= 0).length)

async function load() {
  loading.value = true
  try {
    const query: PageQuery = {
      page: Number(params.page), pageSize: 20,
      search: String(params.search), feedType: String(params.feedType || ''),
    }
    if (params.enabled !== '') query.enabled = String(params.enabled)
    const result = await feedBatchApi.list(query)
    batches.value = result.items
    total.value = result.total
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editingId.value = null
  Object.assign(form, emptyForm())
  editorOpen.value = true
}

function openEdit(batch: FeedBatch) {
  editingId.value = batch.id
  Object.assign(form, {
    batchNumber: batch.batchNumber, feedType: batch.feedType, inboundAmountKg: batch.inboundAmountKg,
    expiryDate: toLocalDateInput(new Date(batch.expiryDate)), enabled: batch.enabled, notes: batch.notes,
  })
  editorOpen.value = true
}

async function save() {
  if (!form.batchNumber || !form.feedType || form.inboundAmountKg <= 0 || !form.expiryDate) {
    ElMessage.warning('请完整填写批号、类型、入库量和到期日')
    return
  }
  saving.value = true
  try {
    const payload = { ...form, expiryDate: toISO(form.expiryDate) }
    if (editingId.value) await feedBatchApi.update(editingId.value, payload)
    else await feedBatchApi.create(payload)
    ElMessage.success(editingId.value ? '饲料批次已更新' : '饲料批次已登记')
    editorOpen.value = false
    await load()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}

async function toggleEnabled(batch: FeedBatch) {
  saving.value = true
  try {
    await feedBatchApi.update(batch.id, {
      batchNumber: batch.batchNumber, feedType: batch.feedType, inboundAmountKg: batch.inboundAmountKg,
      expiryDate: batch.expiryDate, enabled: !batch.enabled, notes: batch.notes,
    })
    ElMessage.success(batch.enabled ? '批次已停用' : '批次已启用')
    await load()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}

async function showConsumptions(batch: FeedBatch) {
  target.value = batch
  consumptionOpen.value = true
  consumptionLoading.value = true
  consumptions.value = []
  try {
    const result = await feedBatchApi.consumptions(batch.id)
    consumptions.value = result.items
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    consumptionLoading.value = false
  }
}

let timer: number | undefined
watch(params, () => { window.clearTimeout(timer); timer = window.setTimeout(load, 200) }, { deep: true })
onMounted(load)
</script>

<template>
  <div class="page-stack">
    <section class="metrics-grid">
      <MetricCard label="批次总数" :value="total" :icon="Box" />
      <MetricCard label="可用批次" :value="enabledCount" :icon="CircleCheck" tone="green" hint="启用且未过期" />
      <MetricCard label="7 日内到期" :value="nearExpiryCount" :icon="Warning" tone="amber" hint="优先安排投喂" />
      <MetricCard label="已过期批次" :value="expiredCount" :icon="Warning" tone="red" />
      <MetricCard label="余量清零" :value="zeroStockCount" :icon="Box" tone="amber" hint="启用中已用尽" />
    </section>
    <section class="workspace-panel">
      <div class="panel-toolbar">
        <div class="filters">
          <el-input v-model="params.search" clearable placeholder="搜索批号或饲料类型" style="width: 240px" />
          <el-input v-model="params.feedType" clearable placeholder="按类型筛选" style="width: 180px" />
          <el-select v-model="params.enabled" placeholder="全部状态" clearable style="width: 140px">
            <el-option label="启用中" value="true" /><el-option label="已停用" value="false" />
          </el-select>
        </div>
        <el-button v-if="canOperate()" type="primary" :icon="Plus" @click="openCreate">登记批次</el-button>
      </div>
      <el-table v-loading="loading" :data="batches" stripe empty-text="暂无饲料批次">
        <el-table-column label="批号 / 类型" min-width="180"><template #default="{ row }"><div class="primary-cell"><strong>{{ row.batchNumber }}</strong><small>{{ row.feedType }}</small></div></template></el-table-column>
        <el-table-column label="入库量" min-width="100"><template #default="{ row }">{{ formatNumber(row.inboundAmountKg) }} kg</template></el-table-column>
        <el-table-column label="剩余 / 已用" min-width="160"><template #default="{ row }">
          <span :class="{ 'stock-zero': row.remainingKg <= 0 }">{{ formatNumber(row.remainingKg) }} / {{ formatNumber(row.inboundAmountKg - row.remainingKg) }} kg</span>
        </template></el-table-column>
        <el-table-column label="到期日" min-width="180"><template #default="{ row }">
          <span>{{ formatDate(row.expiryDate) }}</span>
          <el-tag v-if="isExpired(row)" type="danger" size="small" effect="plain" round style="margin-left: 6px">已过期</el-tag>
          <el-tag v-else-if="isNearExpiry(row)" type="warning" size="small" effect="plain" round style="margin-left: 6px">临期</el-tag>
        </template></el-table-column>
        <el-table-column label="状态" width="100"><template #default="{ row }">
          <el-tag v-if="!row.enabled" type="info" effect="light" round>已停用</el-tag>
          <StatusBadge v-else status="active" />
        </template></el-table-column>
        <el-table-column label="备注" prop="notes" min-width="140" show-overflow-tooltip />
        <el-table-column label="操作" width="230" fixed="right"><template #default="{ row }">
          <el-button link type="primary" :icon="Link" @click="showConsumptions(row)">关联投喂</el-button>
          <template v-if="canOperate()">
            <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button link :type="row.enabled ? 'warning' : 'success'" :loading="saving" @click="toggleEnabled(row)">{{ row.enabled ? '停用' : '启用' }}</el-button>
          </template>
        </template></el-table-column>
      </el-table>
      <div class="pagination"><el-pagination v-model:current-page="params.page" layout="total, prev, pager, next" :total="total" :page-size="20" /></div>
      <el-alert title="执行完成时按「先到期先出」自动从同类型启用批次扣减；页内剩余库存为当前页合计。" type="info" :closable="false" show-icon style="margin-top: 12px" />
    </section>
    <el-dialog v-model="editorOpen" :title="editingId ? '编辑饲料批次' : '登记饲料批次'" width="600px">
      <el-alert v-if="editingId" title="已有投喂消耗的批次，饲料类型不能修改，入库量只能补登增加；历史扣减不可修改。" type="warning" :closable="false" show-icon style="margin-bottom: 12px" />
      <el-form label-position="top" class="form-grid">
        <el-form-item label="批号"><el-input v-model="form.batchNumber" placeholder="例：FB-20260901-01" /></el-form-item>
        <el-form-item label="饲料类型"><el-input v-model="form.feedType" placeholder="须与计划饲料类型一致，如 配合饲料" /></el-form-item>
        <el-form-item label="入库量（kg）"><el-input-number v-model="form.inboundAmountKg" :min="0.1" :step="10" /></el-form-item>
        <el-form-item label="到期日"><el-date-picker v-model="form.expiryDate" type="date" value-format="YYYY-MM-DD" /></el-form-item>
        <el-form-item label="启用状态" class="form-span"><el-switch v-model="form.enabled" active-text="启用，可被先到期先出扣减" inactive-text="停用，不参与扣减" /></el-form-item>
        <el-form-item label="备注" class="form-span"><el-input v-model="form.notes" type="textarea" :rows="3" placeholder="供应商、存放库位等" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="editorOpen = false">取消</el-button><el-button type="primary" :loading="saving" @click="save">保存</el-button></template>
    </el-dialog>
    <el-dialog v-model="consumptionOpen" :title="`关联投喂 · ${target?.batchNumber || ''}`" width="760px">
      <el-table v-loading="consumptionLoading" :data="consumptions" stripe empty-text="该批次暂无投喂消耗">
        <el-table-column label="执行 #" width="90"><template #default="{ row }">#{{ row.controlExecutionId }}</template></el-table-column>
        <el-table-column label="养殖池" min-width="130"><template #default="{ row }">{{ row.controlExecution?.pond?.name || '—' }}</template></el-table-column>
        <el-table-column label="安排时间" min-width="155"><template #default="{ row }">{{ formatDateTime(row.controlExecution?.scheduledAt) }}</template></el-table-column>
        <el-table-column label="完成时间" min-width="155"><template #default="{ row }">{{ formatDateTime(row.controlExecution?.completedAt) }}</template></el-table-column>
        <el-table-column label="操作人" min-width="100"><template #default="{ row }">{{ row.controlExecution?.operator || '—' }}</template></el-table-column>
        <el-table-column label="消耗量" min-width="110"><template #default="{ row }">{{ formatNumber(row.amountKg, 3) }} kg</template></el-table-column>
      </el-table>
    </el-dialog>
  </div>
</template>
