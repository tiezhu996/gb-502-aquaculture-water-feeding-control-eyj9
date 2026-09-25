package router

import (
	"aquaculture-water-feeding-control/backend/internal/config"
	"aquaculture-water-feeding-control/backend/internal/constants"
	"aquaculture-water-feeding-control/backend/internal/handler"
	"aquaculture-water-feeding-control/backend/internal/model"
	"aquaculture-water-feeding-control/backend/internal/repository"
	"aquaculture-water-feeding-control/backend/internal/service"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const testJWTSecret = "test-secret-key-at-least-32-characters-long!!"

func testConfig() config.Config {
	return config.Config{Environment: "test", HTTPAddr: ":0", JWTSecret: testJWTSecret, TokenTTL: time.Hour, CORSOrigins: []string{"*"}, RateLimit: 100000}
}

func issueToken(t *testing.T, userID uint, username, displayName, role string) string {
	t.Helper()
	now := time.Now().UTC()
	claims := service.TokenClaims{
		UserID: userID, Username: username, DisplayName: displayName, Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: "aquaculture-control", Subject: fmt.Sprintf("%d", userID),
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

type httpFixture struct {
	engine *gin.Engine
	db     *gorm.DB
	tokens map[string]string
}

func newHTTPFixture(t *testing.T) *httpFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:api-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		if strings.Contains(err.Error(), "CGO_ENABLED=0") {
			t.Skip("sqlite 集成测试需要 CGO，已跳过")
		}
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.Pond{}, &model.WaterReading{}, &model.FeedingPlan{},
		&model.ControlExecution{}, &model.AuditLog{}, &model.FeedBatch{}, &model.FeedConsumption{},
	); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	users := []model.User{
		{Username: "admin1", DisplayName: "管理员", PasswordHash: "x", Role: constants.RoleAdmin, Active: true},
		{Username: "viewer1", DisplayName: "观察员", PasswordHash: "x", Role: constants.RoleViewer, Active: true},
	}
	for i := range users {
		if err := db.Create(&users[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	pond := model.Pond{Code: "P-H1", Name: "HTTP塘", Species: "对虾", AreaSquareMeters: 1000, CapacityKg: 5000, GrowthStage: "成长期", Status: constants.PondStatusActive, Manager: "测试"}
	db.Create(&pond)
	plan := model.FeedingPlan{
		PondID: pond.ID, Name: "HTTP计划", Version: 1, DailyAmountKg: 100, FrequencyPerDay: 2,
		FeedType: "配合饲料", TargetGrowthStage: "成长期", MinOxygen: 4,
		StartDate: now.Add(-24 * time.Hour), EndDate: now.Add(7 * 24 * time.Hour),
		Status: constants.PlanStatusApproved, CreatedBy: "admin1",
	}
	db.Create(&plan)
	db.Create(&model.WaterReading{PondID: pond.ID, DissolvedOxygen: 7, Temperature: 25, PH: 7.5, Ammonia: 0.1, Turbidity: 20, MeasuredAt: now, Source: "manual", RiskLevel: constants.RiskNormal})

	userRepo := repository.NewUserRepository(db)
	auditService := service.NewAuditService(repository.NewAuditRepository(db))
	authService := service.NewAuthService(userRepo, "test-secret-key-at-least-32-characters-long!!", time.Hour)
	pondService := service.NewPondService(repository.NewPondRepository(db), auditService)
	readingService := service.NewReadingService(repository.NewReadingRepository(db), repository.NewPondRepository(db), auditService)
	planService := service.NewPlanService(repository.NewPlanRepository(db), repository.NewPondRepository(db), repository.NewReadingRepository(db), auditService)
	executionService := service.NewExecutionService(repository.NewExecutionRepository(db), repository.NewPlanRepository(db), repository.NewPondRepository(db), repository.NewReadingRepository(db), repository.NewFeedBatchRepository(db), auditService)
	batchService := service.NewFeedBatchService(repository.NewFeedBatchRepository(db), auditService)

	gin.SetMode(gin.TestMode)
	engine := New(testConfig(), redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"}), authService, Handlers{
		Auth:       handler.NewAuthHandler(authService),
		Health:     handler.NewHealthHandler(db, redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})),
		Ponds:      handler.NewPondHandler(pondService),
		Readings:   handler.NewReadingHandler(readingService),
		Plans:      handler.NewPlanHandler(planService),
		Executions: handler.NewExecutionHandler(executionService),
		Batches:    handler.NewFeedBatchHandler(batchService),
		Audit:      handler.NewAuditHandler(auditService),
	})
	tokens := map[string]string{}
	for _, user := range users {
		tokens[user.Username] = issueToken(t, user.ID, user.Username, user.DisplayName, string(user.Role))
	}
	return &httpFixture{engine: engine, db: db, tokens: tokens}
}

func (f *httpFixture) request(t *testing.T, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	f.engine.ServeHTTP(rec, req)
	return rec
}

func envelopeError(rec *httptest.ResponseRecorder) string {
	var payload struct {
		Error struct{ Message string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		return rec.Body.String()
	}
	return payload.Error.Message
}

func TestFeedBatchHTTPFlow(t *testing.T) {
	fx := newHTTPFixture(t)
	admin := fx.tokens["admin1"]

	// viewer 只读：不能登记批次
	rec := fx.request(t, http.MethodPost, "/api/feed-batches", fx.tokens["viewer1"], map[string]any{
		"batchNumber": "FB-HTTP-1", "feedType": "配合饲料", "inboundAmountKg": 100, "expiryDate": time.Now().Add(72 * time.Hour).Format(time.RFC3339),
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer create batch should be 403, got %d", rec.Code)
	}

	// 未认证拒绝
	rec = fx.request(t, http.MethodGet, "/api/feed-batches", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list should be 401, got %d", rec.Code)
	}

	// admin 登记批次
	rec = fx.request(t, http.MethodPost, "/api/feed-batches", admin, map[string]any{
		"batchNumber": "FB-HTTP-1", "feedType": "配合饲料", "inboundAmountKg": 100, "expiryDate": time.Now().Add(72 * time.Hour).Format(time.RFC3339),
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create batch: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data model.FeedBatch `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &created)
	if created.Data.RemainingAmountKg != 100 {
		t.Fatalf("remaining should be 100, got %v", created.Data.RemainingAmountKg)
	}

	// 列表可按类型与启用状态过滤
	rec = fx.request(t, http.MethodGet, "/api/feed-batches?feedType=%E9%85%8D%E5%90%88%E9%A5%B2%E6%96%99&enabled=true", admin, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}

	// 无匹配启用批次时，安排执行再完成会被 409 拒绝
	rec = fx.request(t, http.MethodPost, "/api/executions", admin, map[string]any{
		"pondId": 1, "feedingPlanId": 1, "plannedAmountKg": 40,
		"scheduledAt": time.Now().Add(time.Hour).Format(time.RFC3339), "weather": "晴",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("schedule execution: %d %s", rec.Code, rec.Body.String())
	}
	var execCreated struct {
		Data model.ControlExecution `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &execCreated)

	// 新建一个只有「膨化饲料」的环境：停用配合饲料批次，触发停用拒绝
	rec = fx.request(t, http.MethodPut, fmt.Sprintf("/api/feed-batches/%d", created.Data.ID), admin, map[string]any{
		"batchNumber": "FB-HTTP-1", "feedType": "配合饲料", "inboundAmountKg": 100,
		"expiryDate": time.Now().Add(72 * time.Hour).Format(time.RFC3339), "enabled": false,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("disable batch: %d %s", rec.Code, rec.Body.String())
	}
	rec = fx.request(t, http.MethodPatch, fmt.Sprintf("/api/executions/%d/complete", execCreated.Data.ID), admin, map[string]any{
		"actualAmountKg": 10, "oxygenSnapshot": 7, "feedback": "测试停用批次拒绝完成的场景",
	})
	if rec.Code != http.StatusConflict || !strings.Contains(envelopeError(rec), "停用") {
		t.Fatalf("disabled batch completion should be 409 about 停用, got %d %s", rec.Code, rec.Body.String())
	}

	// 重新启用后完成成功
	rec = fx.request(t, http.MethodPut, fmt.Sprintf("/api/feed-batches/%d", created.Data.ID), admin, map[string]any{
		"batchNumber": "FB-HTTP-1", "feedType": "配合饲料", "inboundAmountKg": 100,
		"expiryDate": time.Now().Add(72 * time.Hour).Format(time.RFC3339), "enabled": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("enable batch: %d %s", rec.Code, rec.Body.String())
	}
	rec = fx.request(t, http.MethodPatch, fmt.Sprintf("/api/executions/%d/complete", execCreated.Data.ID), admin, map[string]any{
		"actualAmountKg": 30, "oxygenSnapshot": 7, "feedback": "正常投喂，库存按 FEFO 自动扣减",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("complete execution: %d %s", rec.Code, rec.Body.String())
	}
	var completed struct {
		Data model.ControlExecution `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &completed)
	if len(completed.Data.Consumptions) != 1 || completed.Data.Consumptions[0].AmountKg != 30 {
		t.Fatalf("completed execution should carry 1 consumption line of 30kg, got %+v", completed.Data.Consumptions)
	}

	// 重复完成只扣一次
	rec = fx.request(t, http.MethodPatch, fmt.Sprintf("/api/executions/%d/complete", execCreated.Data.ID), admin, map[string]any{
		"actualAmountKg": 30, "oxygenSnapshot": 7, "feedback": "重复提交必须被拒绝",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("second completion should be 409, got %d %s", rec.Code, rec.Body.String())
	}

	// 批次页能查到关联投喂
	rec = fx.request(t, http.MethodGet, fmt.Sprintf("/api/feed-batches/%d/consumptions", created.Data.ID), fx.tokens["viewer1"], nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("consumptions readable by viewer: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), fmt.Sprintf(`"controlExecutionId":%d`, execCreated.Data.ID)) {
		t.Fatalf("consumptions response should reference execution, got %s", rec.Body.String())
	}

	// 批次余量已扣减
	rec = fx.request(t, http.MethodGet, fmt.Sprintf("/api/feed-batches/%d", created.Data.ID), admin, nil)
	json.Unmarshal(rec.Body.Bytes(), &created)
	if created.Data.RemainingAmountKg != 70 {
		t.Fatalf("remaining should be 70 after one completion, got %v", created.Data.RemainingAmountKg)
	}
}
