package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pgvector/pgvector-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"aiagent/internal/model"
	"aiagent/internal/service"
)

// 重向量化工具：把 seed 生成的随机向量替换为真实 embedding。
//
// 用法：
//   go run ./cmd/reembed                     # 全量重跑 camera_events + video_scenes
//   go run ./cmd/reembed --table=camera      # 只跑摄像头事件
//   go run ./cmd/reembed --table=video       # 只跑视频场景
//   go run ./cmd/reembed --dry-run           # 只预览将处理的文本，不写库
//   go run ./cmd/reembed --limit=50          # 只处理前 50 条（先小批量验证）
//   go run ./cmd/reembed --batch=10          # 调整每批数量（默认 20）
//
// 说明：seed 数据用 rand.Float32() 填充了 1024 维随机向量，导致向量检索排序无语义。
// 本工具用真实 embedding 模型对 Summary / Description 重新向量化。

type cameraRow struct {
	ID          int64
	Summary     string
	CameraName  string
	PersonDesc  string
	VehicleDesc string
	PackageDesc string
	Action      string
	Zone        string
}

type sceneRow struct {
	ID          int64
	Description string
}

var stats struct {
	ok, fail, skipped int
}

func main() {
	table := flag.String("table", "all", "camera | video | all")
	limit := flag.Int("limit", 0, "只处理前 N 条，0 表示全部")
	batchSize := flag.Int("batch", 20, "每批向量化条数")
	dryRun := flag.Bool("dry-run", false, "只打印将处理的文本，不调用接口也不写库")
	dsn := flag.String("dsn", "host=127.0.0.1 user=postgres password=postgres dbname=aiagent port=5432 sslmode=disable TimeZone=Asia/Shanghai", "数据库连接串")
	flag.Parse()

	db, err := gorm.Open(postgres.Open(*dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		fmt.Println("数据库连接失败:", err)
		os.Exit(1)
	}

	embedSvc := service.NewEmbeddingService()
	ctx := context.Background()

	// 解析向量模型配置（与线上 resolveEmbedModelConfig 一致：向量模型 → 回退对话模型 → 兜底）
	mcfg := resolveEmbedConfig(db)
	if mcfg == nil || mcfg.APIKey == "" {
		fmt.Println("未找到可用的向量模型配置，请先在「系统设置 → 模型配置」中配置并激活一个向量模型")
		os.Exit(1)
	}
	fmt.Printf("使用模型: %s  (BaseURL=%s)\n\n", mcfg.ModelName, mcfg.BaseURL)

	if *dryRun {
		fmt.Println("!!! dry-run 模式：只预览，不调用接口、不写库 !!!\n")
	}

	switch *table {
	case "camera", "all":
		reembedCamera(ctx, db, embedSvc, mcfg, *batchSize, *limit, *dryRun)
	case "video":
		// 单独跑 video 时也要执行，见下方统一处理
	}

	if *table == "video" || *table == "all" {
		reembedVideo(ctx, db, embedSvc, mcfg, *batchSize, *limit, *dryRun)
	}

	fmt.Printf("\n===== 完成 =====\n成功 %d 条，失败 %d 条，跳过(空文本) %d 条\n", stats.ok, stats.fail, stats.skipped)
}

// resolveEmbedConfig 从数据库取向量模型配置，取不到则回退对话模型。
func resolveEmbedConfig(db *gorm.DB) *service.ModelConfig {
	var cfg model.ModelConfig
	if err := db.Where("model_type = ? AND is_active = ? AND is_deleted = ?", model.ModelTypeEmbedding, true, false).
		First(&cfg).Error; err == nil && cfg.APIKey != "" {
		return &service.ModelConfig{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, ModelName: cfg.ModelName}
	}
	var chat model.ModelConfig
	if err := db.Where("model_type = ? AND is_active = ? AND is_deleted = ?", model.ModelTypeChat, true, false).
		First(&chat).Error; err == nil && chat.APIKey != "" {
		fmt.Println("提示: 未找到向量模型，回退复用对话模型配置")
		return &service.ModelConfig{BaseURL: chat.BaseURL, APIKey: chat.APIKey, ModelName: chat.ModelName}
	}
	return nil
}

// tableDim 读取表当前向量的维度，用于校验模型输出维度是否匹配。
func tableDim(db *gorm.DB, table string) int {
	var dim int
	if err := db.Raw(fmt.Sprintf("SELECT vector_dims(embedding) FROM %s WHERE embedding IS NOT NULL LIMIT 1", table)).Scan(&dim).Error; err != nil {
		return 0
	}
	return dim
}

// buildCameraText 构造用于向量化的文本：优先用 Summary，缺失时用结构化标签拼接。
func buildCameraText(r cameraRow) string {
	if strings.TrimSpace(r.Summary) != "" {
		return r.Summary
	}
	var parts []string
	if r.CameraName != "" {
		parts = append(parts, "摄像头:"+r.CameraName)
	}
	if r.PersonDesc != "" {
		parts = append(parts, "人物:"+r.PersonDesc)
	}
	if r.VehicleDesc != "" {
		parts = append(parts, "车辆:"+r.VehicleDesc)
	}
	if r.PackageDesc != "" {
		parts = append(parts, "包裹:"+r.PackageDesc)
	}
	if r.Action != "" {
		parts = append(parts, "动作:"+r.Action)
	}
	if r.Zone != "" {
		parts = append(parts, "区域:"+r.Zone)
	}
	return strings.Join(parts, " | ")
}

func reembedCamera(ctx context.Context, db *gorm.DB, svc *service.EmbeddingService, mcfg *service.ModelConfig, batchSize, limit int, dryRun bool) {
	var rows []cameraRow
	q := db.Raw(`SELECT id, summary, camera_name, person_desc, vehicle_desc, package_desc, action, zone
	             FROM camera_events ORDER BY id`)
	if limit > 0 {
		q = db.Raw(`SELECT id, summary, camera_name, person_desc, vehicle_desc, package_desc, action, zone
		            FROM camera_events ORDER BY id LIMIT ?`, limit)
	}
	if err := q.Scan(&rows).Error; err != nil {
		fmt.Println("读取 camera_events 失败:", err)
		return
	}
	fmt.Printf("=== camera_events: 共 %d 条 ===\n", len(rows))
	dim := tableDim(db, "camera_events")
	if dim > 0 {
		fmt.Printf("表当前向量维度: %d（模型输出维度必须一致，否则会报错）\n", dim)
	}

	checked := false
	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		batch := rows[i:end]

		texts := make([]string, 0, len(batch))
		ids := make([]int64, 0, len(batch))
		for _, r := range batch {
			t := strings.TrimSpace(buildCameraText(r))
			if t == "" {
				stats.skipped++
				continue
			}
			texts = append(texts, t)
			ids = append(ids, r.ID)
		}
		if len(texts) == 0 {
			continue
		}

		if dryRun {
			for j, t := range texts {
				preview := t
				if len([]rune(preview)) > 80 {
					preview = string([]rune(preview)[:80]) + "…"
				}
				fmt.Printf("  [dry-run] id=%d %s\n", ids[j], preview)
			}
			continue
		}

		embs, err := svc.Embed(ctx, texts, mcfg)
		if err != nil {
			fmt.Printf("  批次 %d-%d 向量化失败: %v\n", i, end, err)
			stats.fail += len(texts)
			continue
		}

		// 首次校验维度
		if !checked && len(embs) > 0 && dim > 0 && len(embs[0]) != dim {
			fmt.Printf("\n错误: 模型输出维度 %d 与表定义维度 %d 不一致，已中止。\n", len(embs[0]), dim)
			fmt.Println("请改用输出维度匹配的 embedding 模型，或修改表字段维度后重跑。")
			os.Exit(1)
		}
		checked = true

		if err := writeEmbeddings(db, "camera_events", ids, embs); err != nil {
			fmt.Printf("  批次 %d-%d 写库失败: %v\n", i, end, err)
			stats.fail += len(texts)
			continue
		}
		stats.ok += len(texts)
		fmt.Printf("  已处理 %d/%d\n", end, len(rows))
		time.Sleep(100 * time.Millisecond) // 温和限速，避免触发网关限流
	}
}

func reembedVideo(ctx context.Context, db *gorm.DB, svc *service.EmbeddingService, mcfg *service.ModelConfig, batchSize, limit int, dryRun bool) {
	var rows []sceneRow
	q := db.Raw(`SELECT id, description FROM video_scenes ORDER BY id`)
	if limit > 0 {
		q = db.Raw(`SELECT id, description FROM video_scenes ORDER BY id LIMIT ?`, limit)
	}
	if err := q.Scan(&rows).Error; err != nil {
		fmt.Println("读取 video_scenes 失败:", err)
		return
	}
	fmt.Printf("\n=== video_scenes: 共 %d 条 ===\n", len(rows))
	dim := tableDim(db, "video_scenes")
	if dim > 0 {
		fmt.Printf("表当前向量维度: %d\n", dim)
	}

	checked := false
	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		batch := rows[i:end]

		texts := make([]string, 0, len(batch))
		ids := make([]int64, 0, len(batch))
		for _, r := range batch {
			t := strings.TrimSpace(r.Description)
			if t == "" {
				stats.skipped++
				continue
			}
			texts = append(texts, t)
			ids = append(ids, r.ID)
		}
		if len(texts) == 0 {
			continue
		}

		if dryRun {
			for j, t := range texts {
				preview := t
				if len([]rune(preview)) > 80 {
					preview = string([]rune(preview)[:80]) + "…"
				}
				fmt.Printf("  [dry-run] id=%d %s\n", ids[j], preview)
			}
			continue
		}

		embs, err := svc.Embed(ctx, texts, mcfg)
		if err != nil {
			fmt.Printf("  批次 %d-%d 向量化失败: %v\n", i, end, err)
			stats.fail += len(texts)
			continue
		}

		if !checked && len(embs) > 0 && dim > 0 && len(embs[0]) != dim {
			fmt.Printf("\n错误: 模型输出维度 %d 与表定义维度 %d 不一致，已中止。\n", len(embs[0]), dim)
			os.Exit(1)
		}
		checked = true

		if err := writeEmbeddings(db, "video_scenes", ids, embs); err != nil {
			fmt.Printf("  批次 %d-%d 写库失败: %v\n", i, end, err)
			stats.fail += len(texts)
			continue
		}
		stats.ok += len(texts)
		fmt.Printf("  已处理 %d/%d\n", end, len(rows))
		time.Sleep(100 * time.Millisecond)
	}
}

// writeEmbeddings 把向量写回指定表。
func writeEmbeddings(db *gorm.DB, table string, ids []int64, embs [][]float64) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for i, id := range ids {
			if i >= len(embs) || embs[i] == nil {
				continue
			}
			vec := make([]float32, len(embs[i]))
			for j, v := range embs[i] {
				vec[j] = float32(v)
			}
			if err := tx.Exec(fmt.Sprintf("UPDATE %s SET embedding = ? WHERE id = ?", table),
				pgvector.NewVector(vec), id).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
