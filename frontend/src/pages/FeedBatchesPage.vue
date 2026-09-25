<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Box, CircleCheck, Plus, Search, Timer, Warning } from '@element-plus/icons-vue'
import { feedBatchApi } from '@/api/feedBatches'
import MetricCard from '@/components/common/MetricCard.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { useAuth } from '@/hooks/useAuth'
import { useQueryParams } from '@/hooks/useQueryParams'
import type { FeedBatch, FeedBatchDetail, FeedBatchInput } from '@/types/models'
import { errorMessage } from '@/utils/errors'
import { formatDate, formatDateTime, formatNumber } from '@/utils/format'

const { canManagePonds } = useAuth()
const { params } = useQueryParams({ search: '', status: '', feedType: '', page: 1 })
const loading = ref(false)
const saving = ref(false)
const batches = ref<FeedBatch[]>([])
const total = ref(0)
const feedTypes = ref<string[]>([])
const editorOpen = ref(false)
const deleteOpen = ref(false)
const toggleOpen = ref(false)
const editingId = ref<number | null>(null)
const target = ref<FeedBatch | null>(null)
const toggleEnabled = ref(false)
const detailOpen = ref(false)
const detailLoading = ref(false)
const detail = ref<FeedBatchDetail | null>(null)
const emptyForm = (): FeedBatchInput => ({ batchNo: '', feedType: '', inboundKg: 1, expireDate: '', enabled: true, notes: '' })
const form = reactive<FeedBatchInput>(emptyForm())

const enabledCount = computed(() => batches.value.filter((item) => item.enabled).length)
const expiredCount = computed(() => batches.value.filter((item) => item.expired).length)
const remainingTotal = computed(() => batches.value.reduce((sum, item) => sum + (item.enabled && !item.expired ? item.remainingKg : 0), 0))

async function load() {
  loading.value = true
  try {
    const result = await feedBatchApi.list({
      page: Number(params.page), pageSize: 20, search: String(params.search),
      status: String(params.status), feedType: String(params.feedType) || undefined,
    })
    batches.value = result.items
    total.value = result.total
    feedTypes.value = Array.from(new Set(result.items.map((item) => item.feedType))).sort()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editingId.value = null
  Object.assign(form, emptyForm())
  form.expireDate = new Date(Date.now() + 90 * 86400000).toISOString().slice(0, 10)
  editorOpen.value = true
}

function openEdit(batch: FeedBatch) {
  editingId.value = batch.id
  Object.assign(form, {
    batchNo: batch.batchNo, feedType: batch.feedType, inboundKg: batch.inboundKg,
    expireDate: batch.expireDate.slice(0, 10), enabled: batch.enabled, notes: batch.notes,
  })
  editorOpen.value = true
}

async function save() {
  if (!form.batchNo.trim() || !form.feedType.trim() || form.inboundKg <= 0 || !form.expireDate) {
    ElMessage.warning('请完整填写批号、类型、入库量和到期日')
    return
  }
  saving.value = true
  try {
    if (editingId.value) await feedBatchApi.update(editingId.value, { ...form })
    else await feedBatchApi.create({ ...form })
    ElMessage.success(editingId.value ? '饲料批次已更新' : '饲料批次已登记')
    editorOpen.value = false
    await load()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}

function askToggle(batch: FeedBatch) {
  target.value = batch
  toggleEnabled.value = !batch.enabled
  toggleOpen.value = true
}

async function confirmToggle() {
  if (!target.value) return
  saving.value = true
  try {
    await feedBatchApi.setEnabled(target.value.id, toggleEnabled.value)
    ElMessage.success(toggleEnabled.value ? '批次已启用' : '批次已停用')
    toggleOpen.value = false
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
    await feedBatchApi.remove(target.value.id)
    ElMessage.success('饲料批次已删除')
    deleteOpen.value = false
    await load()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}

async function showDetail(batch: FeedBatch) {
  detailOpen.value = true
  detailLoading.value = true
  detail.value = null
  try {
    detail.value = await feedBatchApi.get(batch.id)
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    detailLoading.value = false
  }
}

let refreshTimer: number | undefined
watch(params, () => {
  window.clearTimeout(refreshTimer)
  refreshTimer = window.setTimeout(load, 250)
}, { deep: true })
onMounted(load)
</script>

<template>
  <div class="page-stack">
    <section class="metrics-grid">
      <MetricCard label="批次总数" :value="total" :icon="Box" hint="饲料台账" />
      <MetricCard label="启用中" :value="enabledCount" :icon="CircleCheck" tone="blue" hint="参与先到期先出" />
      <MetricCard label="已过期" :value="expiredCount" :icon="Warning" tone="red" hint="过期自动拦截" />
      <MetricCard label="可用库存合计" :value="`${formatNumber(remainingTotal)} kg`" :icon="Timer" tone="green" hint="启用且未过期剩余" />
    </section>
    <section class="workspace-panel">
      <div class="panel-toolbar">
        <div class="filters">
          <el-input v-model="params.search" clearable placeholder="搜索批号或饲料类型" :prefix-icon="Search" />
          <el-select v-model="params.feedType" placeholder="全部类型" clearable>
            <el-option v-for="type in feedTypes" :key="type" :label="type" :value="type" />
          </el-select>
          <el-select v-model="params.status" placeholder="全部状态" clearable>
            <el-option label="启用" value="enabled" /><el-option label="停用" value="disabled" />
          </el-select>
        </div>
        <el-button v-if="canManagePonds()" type="primary" :icon="Plus" @click="openCreate">登记批次</el-button>
      </div>
      <el-table v-loading="loading" :data="batches" stripe empty-text="暂无饲料批次">
        <el-table-column label="批号 / 类型" min-width="220">
          <template #default="{ row }">
            <div class="primary-cell">
              <button class="inline-link" @click="showDetail(row)"><strong>{{ row.batchNo }}</strong></button>
              <small>{{ row.feedType }}</small>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="到期日" min-width="130">
          <template #default="{ row }">
            <span>{{ formatDate(row.expireDate) }}</span>
            <el-tag v-if="row.expired" type="danger" size="small" effect="plain" round style="margin-left: 6px">已过期</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="入库量" min-width="100"><template #default="{ row }">{{ formatNumber(row.inboundKg, 2) }} kg</template></el-table-column>
        <el-table-column label="已消耗 / 剩余" min-width="150">
          <template #default="{ row }">
            <span :class="{ 'text-danger': row.remainingKg <= 0 }">{{ formatNumber(row.consumedKg, 2) }} / {{ formatNumber(row.remainingKg, 2) }} kg</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'" effect="light" round>{{ row.enabled ? '启用' : '停用' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column v-if="canManagePonds()" label="操作" width="210" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button link :type="row.enabled ? 'warning' : 'success'" @click="askToggle(row)">{{ row.enabled ? '停用' : '启用' }}</el-button>
            <el-button link type="danger" @click="target = row; deleteOpen = true">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="pagination"><el-pagination v-model:current-page="params.page" layout="total, prev, pager, next" :total="total" :page-size="20" /></div>
    </section>
    <el-dialog v-model="editorOpen" :title="editingId ? '编辑饲料批次' : '登记饲料批次'" width="620px">
      <el-alert v-if="editingId" title="批次一旦发生投喂消耗，批号、类型和入库量将锁定不可修改；到期日和启用状态可继续维护。" type="info" :closable="false" show-icon />
      <el-form label-position="top" class="form-grid form-with-alert">
        <el-form-item label="批号"><el-input v-model="form.batchNo" placeholder="FB-2026-0901" /></el-form-item>
        <el-form-item label="饲料类型"><el-input v-model="form.feedType" placeholder="与投喂计划饲料类型完全一致" /></el-form-item>
        <el-form-item label="入库量（kg）"><el-input-number v-model="form.inboundKg" :min="0.1" :precision="2" :step="10" /></el-form-item>
        <el-form-item label="到期日"><el-date-picker v-model="form.expireDate" type="date" value-format="YYYY-MM-DD" /></el-form-item>
        <el-form-item label="启用状态"><el-switch v-model="form.enabled" active-text="启用，参与投喂扣减" inactive-text="停用" /></el-form-item>
        <el-form-item label="备注" class="form-span"><el-input v-model="form.notes" type="textarea" :rows="3" placeholder="供应商、仓位等补充信息" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="editorOpen = false">取消</el-button><el-button type="primary" :loading="saving" @click="save">保存</el-button></template>
    </el-dialog>
    <el-drawer v-model="detailOpen" title="批次关联投喂" size="560px">
      <div v-loading="detailLoading">
        <template v-if="detail">
          <el-descriptions :column="2" border>
            <el-descriptions-item label="批号">{{ detail.batchNo }}</el-descriptions-item>
            <el-descriptions-item label="类型">{{ detail.feedType }}</el-descriptions-item>
            <el-descriptions-item label="入库量">{{ formatNumber(detail.inboundKg, 2) }} kg</el-descriptions-item>
            <el-descriptions-item label="到期日">{{ formatDate(detail.expireDate) }}</el-descriptions-item>
            <el-descriptions-item label="已消耗">{{ formatNumber(detail.consumedKg, 2) }} kg</el-descriptions-item>
            <el-descriptions-item label="剩余">{{ formatNumber(detail.remainingKg, 2) }} kg</el-descriptions-item>
          </el-descriptions>
          <h4 style="margin: 18px 0 10px">消耗明细（已完成扣减不可修改）</h4>
          <el-table :data="detail.consumptions" stripe size="small" empty-text="该批次暂无投喂消耗">
            <el-table-column label="完成时间" min-width="150"><template #default="{ row }">{{ formatDateTime(row.completedAt || row.createdAt) }}</template></el-table-column>
            <el-table-column label="养殖池" prop="pondName" min-width="110" show-overflow-tooltip />
            <el-table-column label="投喂计划" prop="planName" min-width="120" show-overflow-tooltip />
            <el-table-column label="扣减量" width="100"><template #default="{ row }">{{ formatNumber(row.amountKg, 2) }} kg</template></el-table-column>
            <el-table-column label="操作人" prop="operator" width="90" show-overflow-tooltip />
          </el-table>
        </template>
      </div>
    </el-drawer>
    <ConfirmDialog v-model="toggleOpen" :title="toggleEnabled ? '启用批次' : '停用批次'" :message="toggleEnabled ? '启用后该批次将参与同类型饲料的先到期先出扣减，确认启用？' : '停用后该批次不再参与投喂扣减，已完成的消耗明细保留不变，确认停用？'" :danger="!toggleEnabled" :loading="saving" @confirm="confirmToggle" />
    <ConfirmDialog v-model="deleteOpen" title="删除饲料批次" :message="`确认删除「${target?.batchNo || ''}」？已发生投喂消耗的批次不能删除。`" danger :loading="saving" @confirm="remove" />
  </div>
</template>
