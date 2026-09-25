package service

import (
	"aquaculture-water-feeding-control/backend/internal/constants"
	"aquaculture-water-feeding-control/backend/internal/dto"
	"aquaculture-water-feeding-control/backend/internal/model"
	"aquaculture-water-feeding-control/backend/internal/repository"
	"math"
	"strconv"
	"time"

	"gorm.io/gorm"
)

type ExecutionService struct {
	repo          *repository.ExecutionRepository
	plans         *repository.PlanRepository
	ponds         *repository.PondRepository
	readings      *repository.ReadingRepository
	batches       *repository.FeedBatchRepository
	audit         *AuditService
	transactional bool
}

func (s *ExecutionService) withinTransaction(fn func(*ExecutionService) error) error {
	return s.audit.WithinTransaction(func(tx *gorm.DB, audit *AuditService) error {
		scoped := &ExecutionService{
			repo: repository.NewExecutionRepository(tx), plans: repository.NewPlanRepository(tx),
			ponds: repository.NewPondRepository(tx), readings: repository.NewReadingRepository(tx),
			batches: repository.NewFeedBatchRepository(tx),
			audit:   audit, transactional: true,
		}
		return fn(scoped)
	})
}

func NewExecutionService(repo *repository.ExecutionRepository, plans *repository.PlanRepository, ponds *repository.PondRepository, readings *repository.ReadingRepository, batches *repository.FeedBatchRepository, audit *AuditService) *ExecutionService {
	return &ExecutionService{repo: repo, plans: plans, ponds: ponds, readings: readings, batches: batches, audit: audit}
}

func (s *ExecutionService) List(query dto.PageQuery, pondID, planID uint) (dto.PageResult[model.ControlExecution], error) {
	query.Normalize()
	items, total, err := s.repo.List(query, pondID, planID)
	if err != nil {
		return dto.PageResult[model.ControlExecution]{}, WrapError(CodeInternal, "查询执行记录失败", err)
	}
	return dto.PageResult[model.ControlExecution]{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, nil
}

func (s *ExecutionService) Get(id uint) (model.ControlExecution, error) {
	var execution model.ControlExecution
	var err error
	if s.transactional {
		execution, err = s.repo.GetForUpdate(id)
	} else {
		execution, err = s.repo.Get(id)
	}
	if err == gorm.ErrRecordNotFound {
		return model.ControlExecution{}, NewError(CodeNotFound, "执行记录不存在")
	}
	if err != nil {
		return model.ControlExecution{}, WrapError(CodeInternal, "查询执行记录失败", err)
	}
	return execution, nil
}

func (s *ExecutionService) Create(input dto.ExecutionInput, actor Actor) (model.ControlExecution, error) {
	if !s.transactional {
		var result model.ControlExecution
		err := s.withinTransaction(func(scoped *ExecutionService) error {
			var inner error
			result, inner = scoped.Create(input, actor)
			return inner
		})
		return result, err
	}
	plan, pond, latest, err := s.validateExecution(input.PondID, input.FeedingPlanID, input.PlannedAmountKg, input.ScheduledAt, 0)
	if err != nil {
		return model.ControlExecution{}, err
	}
	execution := model.ControlExecution{
		PondID: input.PondID, FeedingPlanID: input.FeedingPlanID, ScheduledAt: input.ScheduledAt.UTC(),
		PlannedAmountKg: input.PlannedAmountKg, Status: constants.ExecutionScheduled,
		Operator: actor.DisplayName, Weather: input.Weather, OxygenSnapshot: latest.DissolvedOxygen,
	}
	if execution.Operator == "" {
		execution.Operator = actor.Username
	}
	if err := s.repo.Create(&execution); err != nil {
		return model.ControlExecution{}, WrapError(CodeInternal, "创建执行记录失败", err)
	}
	execution.Pond = &pond
	execution.FeedingPlan = &plan
	if err := s.audit.Record(actor, "schedule", "control_execution", execution.ID, nil, execution, "根据已批准计划安排投喂"); err != nil {
		return model.ControlExecution{}, err
	}
	return execution, nil
}

func (s *ExecutionService) Update(id uint, input dto.UpdateExecutionInput, actor Actor) (model.ControlExecution, error) {
	if !s.transactional {
		var result model.ControlExecution
		err := s.withinTransaction(func(scoped *ExecutionService) error {
			var inner error
			result, inner = scoped.Update(id, input, actor)
			return inner
		})
		return result, err
	}
	execution, err := s.Get(id)
	if err != nil {
		return model.ControlExecution{}, err
	}
	if execution.Status == constants.ExecutionCompleted || execution.Status == constants.ExecutionCancelled {
		return model.ControlExecution{}, NewError(CodeConflict, "已完成或已取消记录不能编辑")
	}
	if input.Status != constants.ExecutionScheduled && input.Status != constants.ExecutionRunning && input.Status != constants.ExecutionCancelled {
		return model.ControlExecution{}, NewError(CodeValidation, "执行状态无效")
	}
	if !execution.Status.CanTransitionTo(input.Status) {
		return model.ControlExecution{}, NewError(CodeConflict, "执行状态不允许逆向迁移")
	}
	if _, _, _, err := s.validateExecution(execution.PondID, execution.FeedingPlanID, input.PlannedAmountKg, input.ScheduledAt, execution.ID); err != nil && input.Status != constants.ExecutionCancelled {
		return model.ControlExecution{}, err
	}
	before := execution
	execution.ScheduledAt = input.ScheduledAt.UTC()
	execution.PlannedAmountKg = input.PlannedAmountKg
	execution.Weather = input.Weather
	if input.Status == constants.ExecutionRunning && execution.StartedAt == nil {
		now := time.Now().UTC()
		execution.StartedAt = &now
	}
	execution.Status = input.Status
	if err := s.repo.Save(&execution); err != nil {
		return model.ControlExecution{}, WrapError(CodeInternal, "更新执行记录失败", err)
	}
	if err := s.audit.Record(actor, "update", "control_execution", execution.ID, before, execution, "调整时间、数量或执行状态"); err != nil {
		return model.ControlExecution{}, err
	}
	return execution, nil
}

func (s *ExecutionService) Complete(id uint, input dto.CompleteExecutionInput, actor Actor) (model.ControlExecution, error) {
	if !s.transactional {
		var result model.ControlExecution
		err := s.withinTransaction(func(scoped *ExecutionService) error {
			var inner error
			result, inner = scoped.Complete(id, input, actor)
			return inner
		})
		return result, err
	}
	execution, err := s.Get(id)
	if err != nil {
		return model.ControlExecution{}, err
	}
	if execution.Status != constants.ExecutionScheduled && execution.Status != constants.ExecutionRunning {
		return model.ControlExecution{}, NewError(CodeConflict, "当前执行状态不能提交反馈")
	}
	if input.OxygenSnapshot < execution.FeedingPlan.MinOxygen {
		return model.ControlExecution{}, NewError(CodeConflict, "现场溶解氧低于计划阈值，请停止投喂并处置水质")
	}
	if input.ActualAmountKg > execution.FeedingPlan.DailyAmountKg {
		return model.ControlExecution{}, NewError(CodeValidation, "单次实际投喂量不能超过计划日投喂量")
	}
	deviation := math.Abs(input.ActualAmountKg-execution.PlannedAmountKg) / execution.PlannedAmountKg
	if deviation > 0.25 && len([]rune(input.Feedback)) < 10 {
		return model.ControlExecution{}, NewError(CodeValidation, "实际量偏差超过 25% 时需提供至少 10 字说明")
	}
	before := execution
	now := time.Now().UTC()
	if execution.StartedAt == nil {
		execution.StartedAt = &now
	}
	execution.CompletedAt = &now
	execution.ActualAmountKg = input.ActualAmountKg
	execution.OxygenSnapshot = input.OxygenSnapshot
	execution.Feedback = input.Feedback
	execution.Status = constants.ExecutionCompleted
	if err := s.repo.Save(&execution); err != nil {
		return model.ControlExecution{}, WrapError(CodeInternal, "完成执行记录失败", err)
	}
	plan, err := s.plans.Get(execution.FeedingPlanID)
	if err != nil {
		return model.ControlExecution{}, WrapError(CodeInternal, "查询关联计划失败", err)
	}
	if err := s.consumeFeed(&execution, plan.FeedType, input.ActualAmountKg, actor); err != nil {
		return model.ControlExecution{}, err
	}
	openCount, err := s.repo.CountOpenForPlanExcluding(plan.ID, execution.ID)
	if err != nil {
		return model.ControlExecution{}, WrapError(CodeInternal, "检查计划待执行记录失败", err)
	}
	if plan.Status == constants.PlanStatusApproved && openCount == 0 {
		planBefore := plan
		plan.Status = constants.PlanStatusExecuted
		if err := s.plans.Save(&plan); err != nil {
			return model.ControlExecution{}, WrapError(CodeInternal, "更新计划执行状态失败", err)
		}
		if err := s.audit.Record(actor, "execute", "feeding_plan", plan.ID, planBefore, plan, "首次投喂执行已完成"); err != nil {
			return model.ControlExecution{}, err
		}
	}
	if err := s.audit.Record(actor, "complete", "control_execution", execution.ID, before, execution, input.Feedback); err != nil {
		return model.ControlExecution{}, err
	}
	return execution, nil
}

func (s *ExecutionService) Delete(id uint, actor Actor) error {
	if !s.transactional {
		return s.withinTransaction(func(scoped *ExecutionService) error { return scoped.Delete(id, actor) })
	}
	execution, err := s.Get(id)
	if err != nil {
		return err
	}
	if execution.Status != constants.ExecutionScheduled {
		return NewError(CodeConflict, "只有待执行记录可以删除")
	}
	if err := s.repo.Delete(&execution); err != nil {
		return WrapError(CodeInternal, "删除执行记录失败", err)
	}
	return s.audit.Record(actor, "delete", "control_execution", execution.ID, execution, nil, "取消未开始的投喂安排")
}

// consumeFeed 按先到期先出（FEFO）从同类型启用批次中扣减实际用量，并写入不可变消耗明细。
// 行锁 + 条件更新保证并发完成时不会超扣；消耗明细的唯一索引保证同一执行只扣一次。
func (s *ExecutionService) consumeFeed(execution *model.ControlExecution, feedType string, amount float64, actor Actor) error {
	existing, err := s.batches.ConsumptionCountForExecution(execution.ID)
	if err != nil {
		return WrapError(CodeInternal, "检查批次消耗明细失败", err)
	}
	if existing > 0 {
		return NewError(CodeConflict, "本次执行已完成饲料扣减，不能重复提交")
	}
	allBatches, err := s.batches.AvailableForUpdate(feedType)
	if err != nil {
		return WrapError(CodeInternal, "查询启用饲料批次失败", err)
	}
	now := time.Now().UTC()
	candidates := make([]model.FeedBatch, 0, len(allBatches))
	var availableTotal float64
	for _, batch := range allBatches {
		if batch.IsExpired(now) {
			continue
		}
		candidates = append(candidates, batch)
		availableTotal += batch.RemainingAmountKg
	}
	if len(allBatches) == 0 {
		total, err := s.batches.CountByType(feedType)
		if err != nil {
			return WrapError(CodeInternal, "查询饲料批次失败", err)
		}
		if total == 0 {
			return NewError(CodeConflict, "没有类型为「"+feedType+"」的饲料批次，无法完成投喂")
		}
		return NewError(CodeConflict, "类型「"+feedType+"」的批次均已停用，无法完成投喂")
	}
	if len(candidates) == 0 {
		return NewError(CodeConflict, "类型「"+feedType+"」的启用批次均已过期，无法完成投喂")
	}
	if availableTotal+1e-6 < amount {
		return NewError(CodeConflict, "同类型启用且未过期批次余量不足，无法完成投喂")
	}

	remaining := amount
	consumedBatches := make([]model.FeedBatch, 0, len(candidates))
	consumptions := make([]model.FeedConsumption, 0, len(candidates))
	for _, batch := range candidates {
		if remaining <= 1e-9 {
			break
		}
		take := batch.RemainingAmountKg
		if take > remaining {
			take = remaining
		}
		rows, err := s.batches.Deduct(batch.ID, take)
		if err != nil {
			return WrapError(CodeInternal, "扣减批次余量失败", err)
		}
		if rows == 0 {
			// 余量在锁定后仍不足（被并发事务先行扣减），整单回滚。
			return NewError(CodeConflict, "批次「"+batch.BatchNumber+"」余量不足，无法完成投喂")
		}
		batch.RemainingAmountKg -= take
		batchCopy := batch
		consumption := model.FeedConsumption{
			ControlExecutionID: execution.ID,
			FeedBatchID:        batch.ID,
			FeedBatch:          &batchCopy,
			FeedType:           feedType,
			AmountKg:           take,
		}
		if err := s.batches.CreateConsumption(&consumption); err != nil {
			return WrapError(CodeInternal, "写入批次消耗明细失败", err)
		}
		consumedBatches = append(consumedBatches, batch)
		consumptions = append(consumptions, consumption)
		remaining -= take
	}
	if remaining > 1e-6 {
		return NewError(CodeConflict, "同类型启用且未过期批次余量不足，无法完成投喂")
	}
	execution.Consumptions = consumptions
	for _, batch := range consumedBatches {
		if err := s.audit.Record(actor, "consume", "feed_batch", batch.ID, nil, batch, "执行 #"+strconv.Itoa(int(execution.ID))+" 按先到期先出扣减饲料"); err != nil {
			return err
		}
	}
	return nil
}

func (s *ExecutionService) validateExecution(pondID, planID uint, amount float64, scheduledAt time.Time, excludedID uint) (model.FeedingPlan, model.Pond, model.WaterReading, error) {
	var plan model.FeedingPlan
	var err error
	if s.transactional {
		plan, err = s.plans.GetForUpdate(planID)
	} else {
		plan, err = s.plans.Get(planID)
	}
	if err == gorm.ErrRecordNotFound {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, NewError(CodeValidation, "投喂计划不存在")
	}
	if err != nil {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, WrapError(CodeInternal, "查询投喂计划失败", err)
	}
	if plan.PondID != pondID {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, NewError(CodeValidation, "投喂计划与养殖池不匹配")
	}
	if plan.Status != constants.PlanStatusApproved {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, NewError(CodeConflict, "只能使用已批准计划安排执行")
	}
	if scheduledAt.Before(plan.StartDate) || scheduledAt.After(plan.EndDate.Add(24*time.Hour)) {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, NewError(CodeValidation, "执行时间必须在计划周期内")
	}
	if amount > plan.DailyAmountKg {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, NewError(CodeValidation, "单次计划量不能超过日投喂量")
	}
	var pond model.Pond
	if s.transactional {
		pond, err = s.ponds.GetForUpdate(pondID)
	} else {
		pond, err = s.ponds.Get(pondID)
	}
	if err != nil {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, WrapError(CodeInternal, "查询养殖池失败", err)
	}
	if pond.Status != constants.PondStatusActive {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, NewError(CodeConflict, "养殖池非运行状态，不能安排投喂")
	}
	latest, err := s.readings.LatestForPond(pondID)
	if err == gorm.ErrRecordNotFound {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, NewError(CodeConflict, "安排执行前必须有水质读数")
	}
	if err != nil {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, WrapError(CodeInternal, "查询最新水质读数失败", err)
	}
	if time.Since(latest.MeasuredAt) > 24*time.Hour {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, NewError(CodeConflict, "最新水质读数已超过 24 小时")
	}
	if latest.DissolvedOxygen < plan.MinOxygen || latest.RiskLevel == constants.RiskCritical {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, NewError(CodeConflict, "当前水质不满足计划执行条件")
	}
	dayStart := time.Date(scheduledAt.UTC().Year(), scheduledAt.UTC().Month(), scheduledAt.UTC().Day(), 0, 0, 0, 0, time.UTC)
	plannedForDay, err := s.repo.PlannedAmountForDay(pondID, dayStart, dayStart.Add(24*time.Hour), excludedID)
	if err != nil {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, WrapError(CodeInternal, "核算当日投喂安排失败", err)
	}
	if plannedForDay+amount > plan.DailyAmountKg+0.0001 {
		return model.FeedingPlan{}, model.Pond{}, model.WaterReading{}, NewError(CodeConflict, "当日累计安排不能超过计划日投喂量")
	}
	return plan, pond, latest, nil
}
