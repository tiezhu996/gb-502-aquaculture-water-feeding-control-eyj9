<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { CircleCheck, Clock, List, Plus, VideoPlay } from '@element-plus/icons-vue'
import { executionApi } from '@/api/executions'
import { planApi } from '@/api/plans'
import { pondApi } from '@/api/ponds'
import { feedBatchApi } from '@/api/feedBatches'
import MetricCard from '@/components/common/MetricCard.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import PlanDrawer from '@/components/common/PlanDrawer.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { useAuth } from '@/hooks/useAuth'
import { useQueryParams } from '@/hooks/useQueryParams'
import type { ControlExecution, ExecutionInput, FeedBatch, FeedingPlan, Pond } from '@/types/models'
import { errorMessage } from '@/utils/errors'
import { formatDate, formatDateTime, formatNumber, toISO, toLocalInput } from '@/utils/format'

const { canOperate } = useAuth()
const { params } = useQueryParams({ status: '', pondId: '', page: 1 })
const executions = ref<ControlExecution[]>([])
const ponds = ref<Pond[]>([])
const plans = ref<FeedingPlan[]>([])
const batches = ref<FeedBatch[]>([])
const total = ref(0)
const loading = ref(false)
const saving = ref(false)
const editorOpen = ref(false)
const completeOpen = ref(false)
const deleteOpen = ref(false)
const drawerOpen = ref(false)
const target = ref<ControlExecution | null>(null)
const selectedPlan = ref<FeedingPlan | null>(null)
const scheduledLocal = ref(toLocalInput(new Date(Date.now() + 3600000)))
const form = reactive<ExecutionInput>({ pondId: 0, feedingPlanId: 0, scheduledAt: '', plannedAmountKg: 0, weather: '' })
const completion = reactive({ actualAmountKg: 0, oxygenSnapshot: 6, feedback: '' })

const scheduledCount = computed(() => executions.value.filter((item) => item.status === 'scheduled').length)
const runningCount = computed(() => executions.value.filter((item) => item.status === 'running').length)
const completedAmount = computed(() => executions.value.filter((item) => item.status === 'completed').reduce((sum, item) => sum + item.actualAmountKg, 0))
const availablePlans = computed(() => plans.value.filter((plan) => plan.status === 'approved' && (!form.pondId || plan.pondId === form.pondId)))

function expiryEnd(batch: FeedBatch): number {
  const end = new Date(batch.expiryDate)
  end.setUTCHours(23, 59, 59, 999)
  return end.getTime()
}

function usableBatches(feedType: string): FeedBatch[] {
  return batches.value
    .filter((batch) => batch.enabled && batch.feedType === feedType && expiryEnd(batch) >= Date.now() && batch.remainingKg > 0)
    .sort((a, b) => expiryEnd(a) - expiryEnd(b) || a.id - b.id)
}

const targetFeedType = computed(() => {
  if (!target.value) return ''
  return target.value.feedingPlan?.feedType || plans.value.find((plan) => plan.id === target.value?.feedingPlanId)?.feedType || ''
})

const fefoAllocation = computed(() => {
  if (!targetFeedType.value || !(completion.actualAmountKg > 0)) return [] as { batch: FeedBatch; take: number; left: number }[]
  let remaining = completion.actualAmountKg
  return usableBatches(targetFeedType.value).map((batch) => {
    const take = Math.min(batch.remainingKg, Math.max(remaining, 0))
    remaining -= take
    return { batch, take, left: batch.remainingKg - take }
  }).filter((item) => item.take > 0)
})

const fefoTotalAvailable = computed(() => usableBatches(targetFeedType.value).reduce((sum, batch) => sum + batch.remainingKg, 0))
const fefoShortage = computed(() => completion.actualAmountKg > fefoTotalAvailable.value)

async function load() {
  loading.value = true
  try {
    const [result, pondResult, planResult, batchResult] = await Promise.all([
      executionApi.list({ page: Number(params.page), pageSize: 20, status: String(params.status), pondId: Number(params.pondId) || undefined }),
      pondApi.list({ page: 1, pageSize: 100 }),
      planApi.list({ page: 1, pageSize: 100 }),
      feedBatchApi.list({ page: 1, pageSize: 100 }),
    ])
    executions.value = result.items
    total.value = result.total
    ponds.value = pondResult.items
    plans.value = planResult.items
    batches.value = batchResult.items
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    loading.value = false
  }
}

function openCreate() {
  const firstPlan = plans.value.find((item) => item.status === 'approved')
  Object.assign(form, {
    pondId: firstPlan?.pondId || 0, feedingPlanId: firstPlan?.id || 0, plannedAmountKg: firstPlan ? firstPlan.dailyAmountKg / firstPlan.frequencyPerDay : 0, weather: '晴朗，微风',
  })
  scheduledLocal.value = toLocalInput(new Date(Date.now() + 3600000))
  editorOpen.value = true
}

function onPlanChange(planId: number) {
  const plan = plans.value.find((item) => item.id === planId)
  if (!plan) return
  form.pondId = plan.pondId
  form.plannedAmountKg = Number((plan.dailyAmountKg / plan.frequencyPerDay).toFixed(2))
}

async function create() {
  if (!form.pondId || !form.feedingPlanId || form.plannedAmountKg <= 0) {
    ElMessage.warning('请选择已批准计划并填写数量')
    return
  }
  saving.value = true
  try {
    await executionApi.create({ ...form, scheduledAt: toISO(scheduledLocal.value) })
    ElMessage.success('投喂执行已安排')
    editorOpen.value = false
    await load()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}

async function start(execution: ControlExecution) {
  saving.value = true
  try {
    await executionApi.update(execution.id, {
      scheduledAt: execution.scheduledAt, plannedAmountKg: execution.plannedAmountKg, weather: execution.weather, status: 'running',
    })
    ElMessage.success('执行已开始')
    await load()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}

function openComplete(execution: ControlExecution) {
  target.value = execution
  Object.assign(completion, { actualAmountKg: execution.plannedAmountKg, oxygenSnapshot: execution.oxygenSnapshot || 6, feedback: '' })
  completeOpen.value = true
}

async function complete() {
  if (!target.value || completion.actualAmountKg <= 0 || completion.feedback.trim().length < 2) {
    ElMessage.warning('请完整填写实际数量、现场溶解氧和反馈')
    return
  }
  if (fefoShortage.value) {
    ElMessage.error('同类型启用批次余量不足，提交将被拒绝，请先补充批次登记')
    return
  }
  saving.value = true
  try {
    await executionApi.complete(target.value.id, { ...completion })
    ElMessage.success('执行反馈已提交，计划状态已同步')
    completeOpen.value = false
    await load()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}

async function remove() {
  if (!target.value) return
  saving.value = true
  try {
    await executionApi.remove(target.value.id)
    ElMessage.success('待执行安排已删除')
    deleteOpen.value = false
    await load()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}

function showPlan(execution: ControlExecution) {
  selectedPlan.value = execution.feedingPlan || plans.value.find((item) => item.id === execution.feedingPlanId) || null
  drawerOpen.value = true
}

function openBatchState(row: ControlExecution): { remaining: number; tone: 'success' | 'warning' | 'danger' } {
  const feedType = row.feedingPlan?.feedType || plans.value.find((plan) => plan.id === row.feedingPlanId)?.feedType || ''
  const remaining = usableBatches(feedType).reduce((sum, batch) => sum + batch.remainingKg, 0)
  if (remaining >= row.plannedAmountKg) return { remaining, tone: 'success' }
  if (remaining > 0) return { remaining, tone: 'warning' }
  return { remaining, tone: 'danger' }
}

let timer: number | undefined
watch(params, () => { window.clearTimeout(timer); timer = window.setTimeout(load, 200) }, { deep: true })
onMounted(load)
</script>

<template>
  <div class="page-stack">
    <section class="metrics-grid">
      <MetricCard label="执行记录" :value="total" :icon="List" />
      <MetricCard label="待执行" :value="scheduledCount" :icon="Clock" tone="amber" />
      <MetricCard label="执行中" :value="runningCount" :icon="VideoPlay" tone="blue" />
      <MetricCard label="页内已投喂" :value="`${formatNumber(completedAmount)} kg`" :icon="CircleCheck" tone="green" />
    </section>
    <section class="workspace-panel">
      <div class="panel-toolbar">
        <div class="filters">
          <el-select v-model="params.pondId" placeholder="全部养殖池" clearable><el-option v-for="pond in ponds" :key="pond.id" :label="pond.name" :value="String(pond.id)" /></el-select>
          <el-select v-model="params.status" placeholder="全部状态" clearable><el-option label="待执行" value="scheduled" /><el-option label="执行中" value="running" /><el-option label="已完成" value="completed" /><el-option label="已取消" value="cancelled" /></el-select>
        </div>
        <el-button v-if="canOperate()" type="primary" :icon="Plus" @click="openCreate">安排执行</el-button>
      </div>
      <el-table v-loading="loading" :data="executions" stripe empty-text="暂无执行记录">
        <el-table-column label="养殖池 / 计划" min-width="230"><template #default="{ row }"><div class="primary-cell"><strong>{{ row.pond?.name }}</strong><button class="inline-link" @click="showPlan(row)">{{ row.feedingPlan?.name }} · v{{ row.feedingPlan?.version }}</button></div></template></el-table-column>
        <el-table-column label="安排时间" min-width="165"><template #default="{ row }">{{ formatDateTime(row.scheduledAt) }}</template></el-table-column>
        <el-table-column label="计划 / 实际" min-width="130"><template #default="{ row }">{{ row.plannedAmountKg }} / {{ row.actualAmountKg || '—' }} kg</template></el-table-column>
        <el-table-column label="饲料 / 本批剩余" min-width="220"><template #default="{ row }">
          <div v-if="row.status === 'completed'">
            <div class="primary-cell"><small>{{ row.feedingPlan?.feedType }}</small></div>
            <div v-if="row.consumptions?.length" class="consume-list">
              <el-popover v-for="item in row.consumptions" :key="item.id" placement="top" :width="240" trigger="hover">
                <template #reference><el-tag size="small" type="success" effect="plain" round style="margin: 2px">{{ item.feedBatch?.batchNumber }} · {{ formatNumber(item.amountKg, 3) }}kg</el-tag></template>
                <div style="font-size: 12px; line-height: 1.8">
                  <div>批号：{{ item.feedBatch?.batchNumber }}</div>
                  <div>类型：{{ item.feedType }}</div>
                  <div>到期日：{{ formatDate(item.feedBatch?.expiryDate) }}</div>
                  <div>当前剩余：{{ formatNumber(item.feedBatch?.remainingKg ?? 0) }} kg</div>
                  <div>本次消耗：{{ formatNumber(item.amountKg, 3) }} kg</div>
                </div>
              </el-popover>
            </div>
            <small v-else>无消耗明细</small>
          </div>
          <template v-else>
            <div class="primary-cell"><small>{{ row.feedingPlan?.feedType }}</small></div>
            <el-tag size="small" :type="openBatchState(row).tone" effect="plain" round>可用剩余 {{ formatNumber(openBatchState(row).remaining) }} kg</el-tag>
          </template>
        </template></el-table-column>
        <el-table-column label="天气" prop="weather" min-width="130" show-overflow-tooltip />
        <el-table-column label="操作人" prop="operator" width="110" />
        <el-table-column label="状态" width="110"><template #default="{ row }"><StatusBadge :status="row.status" /></template></el-table-column>
        <el-table-column v-if="canOperate()" label="操作" width="190" fixed="right"><template #default="{ row }">
          <el-button v-if="row.status === 'scheduled'" link type="primary" :loading="saving" @click="start(row)">开始</el-button>
          <el-button v-if="row.status === 'scheduled' || row.status === 'running'" link type="success" @click="openComplete(row)">提交反馈</el-button>
          <el-button v-if="row.status === 'scheduled'" link type="danger" @click="target = row; deleteOpen = true">删除</el-button>
        </template></el-table-column>
      </el-table>
      <div class="pagination"><el-pagination v-model:current-page="params.page" layout="total, prev, pager, next" :total="total" :page-size="20" /></div>
    </section>
    <el-dialog v-model="editorOpen" title="安排投喂执行" width="620px">
      <el-alert title="仅可选择已批准计划；保存时将检查 24 小时内水质" type="info" :closable="false" show-icon />
      <el-form label-position="top" class="form-grid form-with-alert">
        <el-form-item label="养殖池"><el-select v-model="form.pondId" @change="form.feedingPlanId = 0"><el-option v-for="pond in ponds.filter((item) => item.status === 'active')" :key="pond.id" :label="pond.name" :value="pond.id" /></el-select></el-form-item>
        <el-form-item label="已批准计划"><el-select v-model="form.feedingPlanId" @change="onPlanChange"><el-option v-for="plan in availablePlans" :key="plan.id" :label="`${plan.name} · v${plan.version}`" :value="plan.id" /></el-select></el-form-item>
        <el-form-item label="执行时间"><el-date-picker v-model="scheduledLocal" type="datetime" value-format="YYYY-MM-DDTHH:mm" /></el-form-item>
        <el-form-item label="计划数量（kg）"><el-input-number v-model="form.plannedAmountKg" :min="0.1" :step="1" /></el-form-item>
        <el-form-item label="天气窗口" class="form-span"><el-input v-model="form.weather" placeholder="例：晴朗，微风" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="editorOpen = false">取消</el-button><el-button type="primary" :loading="saving" @click="create">确认安排</el-button></template>
    </el-dialog>
    <el-dialog v-model="completeOpen" title="提交执行反馈" width="620px">
      <el-form label-position="top" class="form-grid">
        <el-form-item label="实际投喂量（kg）"><el-input-number v-model="completion.actualAmountKg" :min="0.1" :step="0.5" /></el-form-item>
        <el-form-item label="现场溶解氧（mg/L）"><el-input-number v-model="completion.oxygenSnapshot" :min="0" :max="30" :step="0.1" /></el-form-item>
        <el-form-item label="执行反馈" class="form-span"><el-input v-model="completion.feedback" type="textarea" :rows="4" placeholder="记录摄食、设备与异常情况" /></el-form-item>
      </el-form>
      <div class="fefo-panel">
        <template v-if="targetFeedType">
          <div class="fefo-head"><strong>先到期先出扣减预览（{{ targetFeedType }}）</strong>
            <el-tag size="small" :type="fefoShortage ? 'danger' : 'success'" effect="plain" round>可用合计 {{ formatNumber(fefoTotalAvailable) }} kg</el-tag>
          </div>
          <el-alert v-if="fefoShortage" type="error" :closable="false" show-icon title="同类型启用且未过期批次余量不足，完成将被拒绝" style="margin: 8px 0" />
          <el-alert v-else-if="!fefoAllocation.length" type="warning" :closable="false" show-icon title="没有匹配的启用批次，请先在饲料批次页登记" style="margin: 8px 0" />
          <el-table v-if="fefoAllocation.length" :data="fefoAllocation" size="small" border>
            <el-table-column label="批号"><template #default="{ row }">{{ row.batch.batchNumber }}</template></el-table-column>
            <el-table-column label="到期日" width="110"><template #default="{ row }">{{ formatDate(row.batch.expiryDate) }}</template></el-table-column>
            <el-table-column label="本次扣减" width="110"><template #default="{ row }">{{ formatNumber(row.take, 3) }} kg</template></el-table-column>
            <el-table-column label="扣后剩余" width="110"><template #default="{ row }">{{ formatNumber(row.left, 3) }} kg</template></el-table-column>
          </el-table>
          <small class="fefo-note">过期、停用、类型不符或余量不足的批次不会参与扣减；提交后扣减不可修改。</small>
        </template>
      </div>
      <template #footer><el-button @click="completeOpen = false">取消</el-button><el-button type="primary" :loading="saving" @click="complete">完成并留痕</el-button></template>
    </el-dialog>
    <ConfirmDialog v-model="deleteOpen" title="删除执行安排" message="只能删除尚未开始的执行安排，确认继续？" danger :loading="saving" @confirm="remove" />
    <PlanDrawer v-model="drawerOpen" :plan="selectedPlan" />
  </div>
</template>
