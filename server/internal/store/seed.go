package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/pgvector/pgvector-go"

	"aiagent/internal/model"
	"aiagent/pkg/ilog"
)

// ensureSeedData 创建种子数据（仅当对应表为空时）。
func (s *Store) ensureSeedData(ctx context.Context) {
	s.seedAgents(ctx)
	s.seedKnowledgeBases(ctx)
	s.seedVideoDatasources(ctx)
	s.seedCameraEvents(ctx)
	s.seedSkillLibrary(ctx)
	s.seedToolLibrary(ctx)
}

// ---------- 智能体 ----------

func (s *Store) seedAgents(ctx context.Context) {
	var count int64
	if s.db.WithContext(ctx).Model(&model.Agent{}).Count(&count); count > 0 {
		return
	}

	agent := &model.Agent{
		Name:        "默认智能体",
		Description: "视频分析智能体，支持摄像头事件搜索和视频内容理解",
		Status:      model.AgentStatusPublished,
		Category:    "video",
		Tags:        "视频,摄像头,监控",
		OwnerName:   "admin",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	s.db.WithContext(ctx).Create(agent)

	// 预设问题
	questions := []string{
		"昨天谁在门口拿过包裹？",
		"穿红色衣服的人路过",
		"找一下经过门口的白色汽车",
		"最近哪天有猫出现在院子里",
	}
	for i, q := range questions {
		s.db.WithContext(ctx).Create(&model.AgentPresetQuestion{
			AgentID:   agent.ID,
			Question:  q,
			SortOrder: i,
			IsActive:  true,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		})
	}
	ilog.Infof("seeded 1 agent + %d preset questions", len(questions))
}

// ---------- 知识库 ----------

func (s *Store) seedKnowledgeBases(ctx context.Context) {
	var count int64
	if s.db.WithContext(ctx).Model(&model.KnowledgeBase{}).Count(&count); count > 0 {
		return
	}

	kbs := []model.KnowledgeBase{
		{Name: "产品文档", Description: "公司产品手册和技术文档", Icon: "📄", Status: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{Name: "会议记录", Description: "团队会议纪要和讨论记录", Icon: "📝", Status: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{Name: "技术博客", Description: "技术文章和开发笔记", Icon: "📚", Status: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}
	for i := range kbs {
		s.db.WithContext(ctx).Create(&kbs[i])
	}

	// 为每个知识库创建文件 + 分块
	seedFiles := []struct {
		kbIdx   int
		name    string
		content string
	}{
		{0, "产品介绍.pdf", "本产品是一个智能视频分析平台，支持摄像头事件检测、视频内容理解和自然语言搜索。核心技术包括：1. 视觉模型分析：自动识别视频中的人物、车辆、宠物、包裹等对象。2. 向量搜索：将视频内容转换为向量嵌入，支持语义搜索。3. 混合搜索：结合结构化过滤条件和向量相似度，实现精准搜索。4. 实时告警：检测异常事件并实时推送通知。系统架构采用前后端分离，后端使用Go语言开发，前端使用Vue3框架，数据库使用PostgreSQL和pgvector向量扩展。"},
		{0, "部署指南.md", "部署步骤：1. 安装Docker和Docker Compose。2. 克隆代码仓库。3. 配置环境变量，包括数据库连接信息和API密钥。4. 运行docker-compose up启动服务。5. 访问http://localhost查看前端页面。系统要求：Linux服务器，至少4GB内存，20GB磁盘空间。需要安装FFmpeg用于视频处理。"},
		{0, "API文档.txt", "API接口列表：POST /api/videos/upload 上传视频文件；GET /api/videos/search 搜索视频内容；POST /api/camera/search 摄像头事件搜索；GET /api/chat/ws WebSocket流式对话；POST /api/agents 创建智能体。所有接口需要JWT鉴权，在请求头中携带Authorization: Bearer token。"},
		{1, "周会议纪要.md", "本周会议讨论内容：1. 视频分析准确率提升到95%，需要优化视觉模型参数。2. 摄像头事件搜索功能开发完成，正在进行测试。3. 前端UI优化，增加摄像头搜索模式。4. 下周计划：部署到生产环境，进行压力测试。5. 新需求：支持多摄像头联动搜索，事件关联分析。参与人员：张三、李四、王五。"},
		{1, "需求评审.md", "新功能需求：1. 多摄像头联动：当在A摄像头检测到人物后，自动关联B摄像头的相关事件。2. 时间线视图：按时间顺序展示所有摄像头事件。3. 统计报表：生成每日/每周/每月的事件统计报告，包含人物、车辆、包裹等维度的数据。4. 导出功能：支持导出搜索结果和统计报告为PDF或Excel格式。"},
		{2, "Go语言并发编程.md", "Go语言的并发编程基于goroutine和channel。goroutine是轻量级线程，由Go运行时管理。使用go关键字即可启动goroutine。channel用于goroutine之间的通信，遵循don't communicate by sharing memory, share memory by communicating的原则。常见的并发模式包括：生产者消费者模式、扇入扇出模式、管道模式等。使用select语句可以同时等待多个channel操作。"},
		{2, "PostgreSQL优化.md", "PostgreSQL性能优化技巧：1. 使用EXPLAIN ANALYZE分析查询计划。2. 创建合适的索引，包括B-tree、Hash、GIN、GiST等类型。3. 使用pgvector的ivfflat索引加速向量搜索。4. 配置shared_buffers、work_mem等参数。5. 使用分区表处理大数据量。6. 定期运行VACUUM和ANALYZE。7. 使用连接池减少连接开销。"},
	}

	for _, f := range seedFiles {
		file := &model.File{
			KnowledgeID:  kbs[f.kbIdx].ID,
			FileName:     f.name,
			FilePath:     "/uploads/docs/" + f.name,
			FileType:     "txt",
			FileSize:     int64(len(f.content)),
			Status:       model.FileStatusReady,
			UploaderName: "admin",
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
		s.db.WithContext(ctx).Create(file)

		// 生成简单分块（每150字）
		runes := []rune(f.content)
		chunkSize := 150
		for i := 0; i < len(runes); i += chunkSize {
			end := i + chunkSize
			if end > len(runes) {
				end = len(runes)
			}
			chunkText := string(runes[i:end])

			vec := make([]float32, 1024)
			for j := range vec {
				vec[j] = rand.Float32()*2 - 1
			}

			s.db.WithContext(ctx).Create(&model.DocumentChunk{
				FileID:      file.ID,
				KnowledgeID: kbs[f.kbIdx].ID,
				ChunkIndex:  i / chunkSize,
				Content:     chunkText,
				ContentLen:  len(chunkText),
				Embedding:   pgvector.NewVector(vec),
				TokenCount:  1024,
				CreatedAt:   time.Now(),
			})
		}
		kbs[f.kbIdx].FileCount++
	}
	s.db.WithContext(ctx).Save(&kbs[0])
	s.db.WithContext(ctx).Save(&kbs[1])
	s.db.WithContext(ctx).Save(&kbs[2])

	ilog.Infof("seeded 3 knowledge bases + %d files", len(seedFiles))
}

// ---------- 视频数据源 ----------

func (s *Store) seedVideoDatasources(ctx context.Context) {
	var count int64
	if s.db.WithContext(ctx).Model(&model.VideoDatasource{}).Count(&count); count > 0 {
		return
	}

	videos := []struct {
		title    string
		scenes   []string
		duration float64
	}{
		{
			title:    "前门监控20240826",
			duration: 120,
			scenes: []string{
				"场景0：前门入口处，一位穿红色外套的男性正在用钥匙开门",
				"场景1：男性进入室内，手里拿着一个棕色纸箱包裹",
				"场景2：门口空无一人，阳光照射在门廊上",
				"场景3：一辆白色轿车缓缓驶过门前道路",
				"场景4：快递员骑着电动车停在门口，放下包裹后离开",
				"场景5：一只橘猫从门前跑过",
				"场景6：女性从室内走出，弯腰拿起门口的快递包裹",
				"场景7：男性在门口停留，查看手机后离开",
				"场景8：一辆黑色SUV停在门口，司机下车按门铃",
				"场景9：送餐员到达门口，将外卖放在门廊上",
			},
		},
		{
			title:    "院子监控20240827",
			duration: 90,
			scenes: []string{
				"场景0：院子的全景画面，阳光明媚",
				"场景1：一只黑色小狗在院子里跑来跑去",
				"场景2：一位女性在院子里浇花",
				"场景3：男性推着自行车从院子里经过",
				"场景4：一只白色波斯猫趴在院子角落",
				"场景5：快递员在院门口放下一个黄色信封包裹",
				"场景6：两个人在院子里交谈",
				"场景7：一辆灰色摩托车停在院子外",
			},
		},
		{
			title:    "车库监控20240825",
			duration: 60,
			scenes: []string{
				"场景0：车库门缓缓打开，一辆白色轿车驶出",
				"场景1：男性从车库中取出工具",
				"场景2：车库门关闭，车辆驶入",
				"场景3：穿蓝色工作服的工人在车库前维修设备",
				"场景4：橙色卡车停在车库门口卸货",
				"场景5：女性从车库中搬出一个大纸箱包裹",
			},
		},
		{
			title:    "门口监控20240824",
			duration: 80,
			scenes: []string{
				"场景0：门口全景，路人经过",
				"场景1：穿绿色卫衣的年轻人快速跑过门口",
				"场景2：一位老人拄着拐杖缓慢走过",
				"场景3：穿黑色夹克的男性在门口停留拍照",
				"场景4：垃圾车停在门口收取垃圾",
				"场景5：一只金毛犬在门口等待主人",
				"场景6：小孩在门口玩耍，追逐蝴蝶",
				"场景7：邮递员将信件投入信箱",
			},
		},
		{
			title:    "后院监控20240823",
			duration: 70,
			scenes: []string{
				"场景0：后院全景，安静无人",
				"场景1：一只花猫从围墙上跳下来",
				"场景2：穿黄色裙子的女性在后院晾衣服",
				"场景3：男性在后院修理水管",
				"场景4：一只泰迪犬在后院散步",
				"场景5：两个小孩在后院玩球",
				"场景6：穿白色衬衫的男性在后院烧烤",
			},
		},
	}

	for _, v := range videos {
		video := &model.VideoDatasource{
			AgentID:      1,
			Title:        v.title,
			FileName:     v.title + ".mp4",
			FilePath:     "/uploads/videos/" + v.title + ".mp4",
			FileSize:     int64(v.duration * 1024 * 1024),
			Duration:     v.duration,
			Status:       model.VideoStatusReady,
			Summary:      fmt.Sprintf("视频「%s」，时长%.0f秒，包含%d个场景", v.title, v.duration, len(v.scenes)),
			ChunkCount:   len(v.scenes),
			SceneCount:   len(v.scenes),
			UploaderName: "admin",
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
		s.db.WithContext(ctx).Create(video)

		for i, desc := range v.scenes {
			vec := make([]float32, 1024)
			for j := range vec {
				vec[j] = rand.Float32()*2 - 1
			}
			s.db.WithContext(ctx).Create(&model.VideoScene{
				VideoID:     video.ID,
				AgentID:     1,
				SceneIndex:  i,
				StartTime:   float64(i * 10),
				EndTime:     float64((i + 1) * 10),
				Duration:    10,
				Description: desc,
				Embedding:   pgvector.NewVector(vec),
				TokenCount:  1024,
				CreatedAt:   time.Now(),
			})
		}
	}

	ilog.Infof("seeded %d video datasources", len(videos))
}

// ---------- 摄像头事件 ----------

func (s *Store) seedCameraEvents(ctx context.Context) {
	var count int64
	if s.db.WithContext(ctx).Model(&model.CameraEvent{}).Count(&count); count > 0 {
		ilog.Infof("camera events already exist: %d", count)
		return
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	cameras := []struct{ id int64; name, zone string }{
		{1, "前门摄像头", "front_door"},
		{2, "院子摄像头", "yard"},
		{3, "车库摄像头", "driveway"},
		{4, "后门摄像头", "entrance"},
		{5, "大门摄像头", "gate"},
	}

	personDescs := []string{"穿红色外套的男性", "穿蓝色T恤的女性", "戴帽子的年轻人", "穿黑色夹克的人", "穿白色衬衫的男性", "穿黄色裙子的女性", "穿运动服的快递员", "穿灰色外套的老人", "穿绿色卫衣的年轻人", "穿紫色羽绒服的女性", "穿橙色马甲的工人", "穿棕色风衣的男性"}
	vehicleTypes := []string{"car", "bike", "truck", "motorcycle", ""}
	vehicleDescs := []string{"白色轿车", "黑色SUV", "红色电动车", "蓝色货车", "灰色摩托车", "银色轿车", "绿色出租车", "白色面包车", "黑色摩托", "红色跑车"}
	petTypes := []string{"cat", "dog", "bird", ""}
	petDescs := []string{"一只橘猫", "一只黑色小狗", "一只白色波斯猫", "一只金毛犬", "一只花猫", "一只泰迪犬", "一只鸟", "一只哈士奇"}
	actions := []string{"walking", "running", "stopped", "picking_up", "delivering", "entering", "leaving"}
	actionDescs := []string{"正在走路经过", "快速跑过", "在门口停留", "弯腰拿起包裹", "正在投递包裹", "进入大门", "离开院子", "在车旁停留", "按门铃", "正在拍摄"}
	colors := []string{"red", "blue", "white", "black", "yellow", "green", "purple", "orange", "gray", "brown"}
	colorDescs := []string{"画面以红色为主", "主要色调偏蓝", "画面明亮", "光线较暗", "黄色调为主", "绿色环境中", "紫色调", "橙色暖光"}
	packageDescs := []string{"一个棕色纸箱", "一个白色塑料袋", "一个小包裹", "一个大纸箱", "一个快递盒", "一个黄色信封", "一个黑色包裹", "一个泡沫箱"}

	now := time.Now()
	total := 1000
	batchSize := 100

	for batch := 0; batch < total; batch += batchSize {
		events := make([]model.CameraEvent, 0, batchSize)
		for i := 0; i < batchSize && batch+i < total; i++ {
			cam := cameras[r.Intn(len(cameras))]
			hasPerson := r.Float64() > 0.2
			hasVehicle := r.Float64() > 0.5
			hasPet := r.Float64() > 0.85
			hasPackage := r.Float64() > 0.6

			colorsList := []string{colors[r.Intn(len(colors))]}
			if r.Float64() > 0.5 {
				colorsList = append(colorsList, colors[r.Intn(len(colors))])
			}

			personDesc := ""
			if hasPerson {
				personDesc = personDescs[r.Intn(len(personDescs))]
			}
			vehicleType := ""
			vehicleDesc := ""
			if hasVehicle {
				vehicleType = vehicleTypes[r.Intn(len(vehicleTypes)-1)]
				vehicleDesc = vehicleDescs[r.Intn(len(vehicleDescs))]
			}
			petType := ""
			petDesc := ""
			if hasPet {
				petType = petTypes[r.Intn(len(petTypes)-1)]
				petDesc = petDescs[r.Intn(len(petDescs))]
			}
			action := actions[r.Intn(len(actions))]
			actionDesc := actionDescs[r.Intn(len(actionDescs))]
			packageDesc := ""
			if hasPackage {
				packageDesc = packageDescs[r.Intn(len(packageDescs))]
			}

			summary := fmt.Sprintf("%s，", cam.name)
			if hasPerson {
				summary += personDesc
			}
			if hasVehicle {
				if hasPerson {
					summary += "，"
				}
				summary += fmt.Sprintf("一辆%s", vehicleDesc)
			}
			if hasPet {
				summary += fmt.Sprintf("，还有%s", petDesc)
			}
			if hasPackage {
				summary += fmt.Sprintf("，地上有%s", packageDesc)
			}
			summary += fmt.Sprintf("，%s", actionDesc)
			summary += fmt.Sprintf("，%s", colorDescs[r.Intn(len(colorDescs))])

			daysAgo := r.Intn(30)
			hoursAgo := r.Intn(24)
			minsAgo := r.Intn(60)
			eventTime := now.Add(-time.Duration(daysAgo)*24*time.Hour -
				time.Duration(hoursAgo)*time.Hour -
				time.Duration(minsAgo)*time.Minute)

			vec := make([]float32, 1024)
			for j := range vec {
				vec[j] = r.Float32()*2 - 1
			}

			personCount := 0
			if hasPerson {
				personCount = r.Intn(3) + 1
			}

			colorsStr := ""
			for j, c := range colorsList {
				if j > 0 {
					colorsStr += ","
				}
				colorsStr += c
			}

			events = append(events, model.CameraEvent{
				CameraID:       cam.id,
				CameraName:     cam.name,
				EventTime:      eventTime,
				Duration:       float64(r.Intn(20) + 5),
				HasPerson:      hasPerson,
				PersonCount:    personCount,
				PersonDesc:     personDesc,
				HasVehicle:     hasVehicle,
				VehicleType:    vehicleType,
				VehicleDesc:    vehicleDesc,
				HasPet:         hasPet,
				PetType:        petType,
				PetDesc:        petDesc,
				HasPackage:     hasPackage,
				PackageDesc:    packageDesc,
				Action:         action,
				ActionDesc:     actionDesc,
				DominantColors: colorsStr,
				ColorDesc:      colorDescs[r.Intn(len(colorDescs))],
				Zone:           cam.zone,
				Summary:        summary,
				VideoPath:      fmt.Sprintf("/uploads/camera/%d.mp4", batch+i+1),
				Embedding:      pgvector.NewVector(vec),
				Processed:      true,
				CreatedAt:      eventTime,
			})
		}
		s.db.CreateInBatches(events, 100)
	}
	ilog.Infof("seeded %d camera events", total)
}

// ---------- 技能库 ----------

func (s *Store) seedSkillLibrary(ctx context.Context) {
	var count int64
	if s.db.WithContext(ctx).Model(&model.SkillLibrary{}).Count(&count); count > 0 {
		return
	}

	skills := []model.SkillLibrary{
		// ---------- 出行规划 ----------
		{
			Name:        "出行规划助手",
			Description: "根据目的地、天数、预算与天气，规划可执行的行程并产出可视化地图页面",
			Summary:     "当用户提出旅行/出游/行程安排需求（涉及目的地、天数、交通方式、预算、天气）时使用",
			Kind:        model.SkillKindPrompt,
			Category:    "出行",
			Content: `你是一位资深旅行规划师。请为用户制定可落地执行的行程方案。

## 第一步：确认必要信息
开始前检查是否已明确，缺失则用一句话集中追问（不要逐条盘问）：
- 目的地、出行天数、同行人数与人群（老人/儿童/情侣/朋友）
- 大致预算、住宿区域偏好
- 偏好取向（人文古建 / 自然山水 / 美食 / 亲子 / 购物）
- 体力强度接受度（日均步行量）

## 第二步：天气与时节判断
- 用 get_time 确认当前日期，判断出行所处季节、是否临近节假日
- 说明目的地该时段的气候特征（气温区间、降水概率、紫外线、台风/梅雨等季节性风险）
- 据此安排：雨天优先室内/半室内项目（博物馆、商圈、展馆），晴天安排户外（环湖、登山、骑行）
- 每个户外半天都给出「下雨替代方案」
- 提醒：天气预报仅可参考 7 天内，出行前 3 天需再次确认

## 第三步：行程编排原则
1. **动线顺路**：同一天景点按地理相邻聚类，避免来回折返
2. **节奏合理**：每天 2-3 个主力景点 + 1 个弹性项目，留足用餐与休息
3. **时段最优**：日出/清晨安排光线与人流最佳的点位，热门景点避开高峰
4. **体力曲线**：强度逐日递减或穿插轻松日，首尾兼顾抵达与返程
5. **餐饮嵌入**：就近推荐当地特色餐，标注人均与是否需要排队
6. **门票预约**：标注需提前预约/抢票的项目及开放时间

## 第四步：输出格式
按天输出，每天包含：
- **概览**：当日主题、天气提示、总步行量预估
- **时间轴表格**：时段 | 地点 | 停留时长 | 交通方式与耗时 | 备注（门票/预约/雨天替代）
- **餐饮安排**：早/午/晚推荐
- **实用提示**：该日注意事项

最后补充：
- **交通总览**：城际抵达/离开方式、市内主要交通（地铁线路、打车场景）
- **预算明细表**：交通 / 住宿 / 门票 / 餐饮 / 其他，给出区间估算
- **打包清单**：按天气给穿衣与装备建议
- **风险提示**：天气、人流、安全等

## 第五步：生成可视化页面
若用户需要地图页面或要求「下载/分享」：
1. 直接在回复中输出完整的单文件 HTML（作为本条消息正文），不要只给链接或摘要
2. 页面要求：手机端自适应、按天分组的景点标记与彩色路线折线、可切换显示
3. 每个景点提供跳转链接：打车、驾车、骑行、公交/步行、在地图 App 中打开（用高德 uri.amap.com 真实地址）
4. 页面里如需「下载 / 分享」入口，固定写 href="__EXPORT_URL__"，后端会自动替换成可分享地址；严禁写 /output/、/files/ 等本地路径
5. 告诉用户：点本条消息的「下载 / 分享」按钮即可拿到可访问链接（产物落在 data/exports，返回 /api/chat/exports/ 地址）

## 工具使用约定
- 若智能体挂载了地图类 MCP 工具（如高德），务必调用它获取真实 POI 坐标与路径规划，不要凭记忆编造经纬度
- 无地图工具时，坐标与耗时需标注「估算，出行前请复核」

## 铁律
- 不确定的开放时间、票价、交通耗时必须标注「需核实」，不编造
- 不推荐用户明确排除的项目
- 行程不要排满，留白也是体验`,
			OwnerName:  "system",
			Visibility: model.AssetVisibilityPublic,
			Status:     1,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		},

		// ---------- 运维助手 ----------
		{
			Name:        "运维助手",
			Description: "排查服务器故障、查看主机指标、执行运维操作，遵循只读优先与最小变更原则",
			Summary:     "当用户需要排查主机/服务故障、查看 CPU/内存/磁盘/网络/进程/服务状态、执行运维命令时使用",
			Kind:        model.SkillKindPrompt,
			Category:    "运维",
			Content: `你是一位资深 SRE。目标是帮用户快速、安全地定位并解决主机与服务问题。

## 核心原则
1. **只读优先**：先用只读工具（host_cpu_info / host_mem_info / host_disk_info / host_network_info / host_process_list / host_service_status / host_env / host_probe / read_file / list_dir）把现场看清楚，再谈变更
2. **最小变更**：能不写就不写；必须变更时，一次只改一处，改完立即验证
3. **可回滚**：write_file 默认开启备份；涉及配置修改先说明原值与目标值
4. **不猜测**：结论必须由命令输出支撑，没查到就说没查到
5. **危险操作**：rm -rf、dd、mkfs、重启服务、kill -9、改 iptables/防火墙、覆盖系统文件——必须先说明影响范围并等待用户确认，不主动执行

## 标准排查流程
1. **明确目标**：先用 list_hosts 确认要操作的主机，不确定就问，绝不猜主机 ID
2. **界定现象**：什么时候开始、影响范围（单机/集群/单服务）、有无变更（发布/配置/扩容）
3. **分层定位**（自上而下，每层用只读工具取证）：
   - 业务层：服务进程是否在、端口是否监听
   - 系统层：CPU（负载、iowait、软中断）、内存（含 swap、OOM）、磁盘（容量、inode、IO 等待）、网络（连通、丢包、DNS）
   - 日志层：定位到具体时间窗的错误日志，取关键行而非全量刷屏
4. **给出结论**：现象 → 根因 → 影响面，证据引用命令输出
5. **修复建议**：分「立即止血」「根本修复」「后续预防」，每条注明风险与回滚方式
6. **验证**：改完复跑关键指标，确认恢复

## 命令使用约定
- exec_command 执行前，简要说明「要跑什么、为什么、预期看到什么」
- 大输出必须过滤：配合 grep / head / tail / awk，不要整份刷屏
- 禁止在一条命令里串联多个破坏性操作
- 长耗时命令先说明可能耗时

## 输出格式
- **结论先行**：一行说清「什么问题、影响多大、是否恢复」
- **证据**：关键命令 + 输出片段（脱敏 IP/密码/密钥）
- **处理**：分步骤给出命令，标注每步风险等级（只读 / 需确认 / 危险）
- **复盘**：根因与后续改进项（监控、告警、容量）

## 铁律
- 不执行用户没有授权范围的命令
- 生产环境不主动重启服务或杀进程
- 不输出密钥、密码、token 等敏感信息，日志引用时脱敏`,
			OwnerName:  "system",
			Visibility: model.AssetVisibilityPublic,
			Status:     1,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		},

		// ---------- 文件检索 ----------
		{
			Name:        "文件检索助手",
			Description: "基于知识库文档做检索问答，答案必须标注出处，找不到就明确说明",
			Summary:     "当用户针对已上传文档/知识库内容提问、需要查找资料或核对原文出处时使用",
			Kind:        model.SkillKindPrompt,
			Category:    "检索",
			Content: `你是一位严谨的资料检索与问答助手。你的所有结论都必须来自 doc_search 的检索结果，不做任何凭记忆的补充。

## 检索策略
1. **拆解问题**：把用户问题拆成可检索的要点，而非整句原样丢给检索
2. **多角度召回**：先用核心关键词检索；结果不足时，换成同义词、缩写、上下位概念再检索 1-2 轮
3. **交叉验证**：涉及数字、日期、人名、结论性表述时，至少两处片段相互印证
4. **适时停止**：连续 2 轮无有效结果就停止检索，如实告知，不要反复空转

## 回答规范
1. **结论先行**：第一句直接回答，再展开细节
2. **逐条标注出处**：每个事实后用「（来源：文档名 / 章节或页码）」标注
3. **原文引用**：关键表述用引用块附上原文片段，方便用户核对
4. **结构化**：多条结论用列表或表格呈现，长内容分段加小标题
5. **区分事实与推断**：检索结果直接支持的写「文档显示」；需要推理的显式说明「由此推断」，不作为确定结论

## 检索不到时
- 明确说「在当前知识库中未检索到相关内容」，不要含糊带过
- 说明已尝试的检索角度，让用户判断是否补充资料或换关键词
- **严禁编造**文档名、章节、数据、结论

## 结果冲突时
- 列出不同文档的各自说法及出处
- 说明可能的差异原因（版本不同、时间不同、适用范围不同）
- 不擅自判定哪个正确，让用户决策或提示需要进一步确认

## 涉及时间的问题
- 用 get_time 获取当前时间，再理解「最近」「上周」「今年」等相对时间表述
- 检索结果若带时间信息，随答案一并给出，便于判断时效性

## 输出模板
**结论**：一句话回答

**依据**：
1. 要点一（来源：xxx）
2. 要点二（来源：xxx）

**补充说明**：适用范围、时效性提醒、待确认事项

## 铁律
- 不引用知识库以外的信息作为事实
- 不虚构来源、不张冠李戴
- 用户问「有没有/是不是」，先给明确的有无判断，再展开`,
			OwnerName:  "system",
			Visibility: model.AssetVisibilityPublic,
			Status:     1,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		},

		// ---------- 工具集合类 ----------
		{
			Name:        "视频搜索工具集",
			Description: "启用全部视频/监控相关搜索工具",
			Summary:     "当用户需要检索摄像头事件、视频片段或监控画面（按内容/时间/对象查找）时使用",
			Kind:        model.SkillKindTool,
			Category:    "工具集",
			Content:     `["search_videos","search_camera","doc_search","get_time"]`,
			OwnerName:   "system",
			Visibility:  model.AssetVisibilityPublic,
			Status:      1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			Name:        "知识库检索工具集",
			Description: "仅启用知识库检索相关工具",
			Summary:     "当用户询问知识库文档内容、需要检索内部资料时使用",
			Kind:        model.SkillKindTool,
			Category:    "工具集",
			Content:     `["doc_search","get_time"]`,
			OwnerName:   "system",
			Visibility:  model.AssetVisibilityPublic,
			Status:      1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
	}

	for i := range skills {
		s.db.WithContext(ctx).Create(&skills[i])
	}

	ilog.Infof("seeded %d skill library items", len(skills))
}

// ---------- 工具库 ----------

func (s *Store) seedToolLibrary(ctx context.Context) {
	metaJSON := func(readOnly, sideEffect, approval bool, resTypes []string) string {
		meta := model.ToolLibraryMetadata{
			ReadOnly: readOnly, SideEffect: sideEffect, ApprovalRequired: approval,
			ResourceTypes: resTypes,
		}
		b, _ := json.Marshal(meta)
		return string(b)
	}
	paramsQueryJSON, _ := json.Marshal(map[string]any{
		"query": map[string]any{"type": "string", "desc": "自然语言检索问题", "required": true},
	})
	paramsReportJSON, _ := json.Marshal(map[string]any{
		"title": map[string]any{"type": "string", "desc": "报告标题", "required": false},
	})
	paramsExecJSON, _ := json.Marshal(map[string]any{
		"host_id": map[string]any{"type": "integer", "desc": "目标主机 ID", "required": true},
		"command": map[string]any{"type": "string", "desc": "要执行的 shell 命令", "required": true},
	})
	paramsPathJSON, _ := json.Marshal(map[string]any{
		"host_id": map[string]any{"type": "integer", "desc": "目标主机 ID", "required": true},
		"path":    map[string]any{"type": "string", "desc": "文件/目录路径", "required": true},
	})
	paramsWriteJSON, _ := json.Marshal(map[string]any{
		"host_id": map[string]any{"type": "integer", "desc": "目标主机 ID", "required": true},
		"path":    map[string]any{"type": "string", "desc": "文件路径", "required": true},
		"content": map[string]any{"type": "string", "desc": "要写入的内容", "required": true},
		"backup":  map[string]any{"type": "boolean", "desc": "是否备份原文件", "required": false},
	})
	paramsServiceJSON, _ := json.Marshal(map[string]any{
		"host_id": map[string]any{"type": "integer", "desc": "目标主机 ID", "required": true},
		"service": map[string]any{"type": "string", "desc": "服务名（仅允许字母数字及 _-.:@）", "required": true},
	})
	paramsProbeJSON, _ := json.Marshal(map[string]any{
		"host_id": map[string]any{"type": "integer", "desc": "目标主机 ID", "required": true},
		"target":  map[string]any{"type": "string", "desc": "探测目标地址（IP/域名）", "required": true},
		"port":    map[string]any{"type": "integer", "desc": "端口（tcp/http 模式使用）", "required": false},
		"mode":    map[string]any{"type": "string", "desc": "探测方式：ping / tcp / http", "required": false},
	})
	paramsDownloadJSON, _ := json.Marshal(map[string]any{
		"host_id": map[string]any{"type": "integer", "desc": "目标主机 ID", "required": true},
		"path":    map[string]any{"type": "string", "desc": "要下载的远程文件路径", "required": true},
	})
	paramsScriptJSON, _ := json.Marshal(map[string]any{
		"host_id":     map[string]any{"type": "integer", "desc": "目标主机 ID", "required": true},
		"script":      map[string]any{"type": "string", "desc": "脚本内容", "required": true},
		"interpreter": map[string]any{"type": "string", "desc": "解释器，默认 bash", "required": false},
	})
	paramsLocalWriteJSON, _ := json.Marshal(map[string]any{
		"path":      map[string]any{"type": "string", "desc": "相对输出根目录的文件路径，如 trip2026/amap.html，必填", "required": true},
		"content":   map[string]any{"type": "string", "desc": "写入的文件内容（HTML/文本）", "required": true},
		"overwrite": map[string]any{"type": "boolean", "desc": "文件已存在时是否覆盖，默认 false", "required": false},
	})

	tools := []model.ToolLibrary{
		{
			Name:        "doc_search",
			Description: "在当前智能体显式绑定的知识库中进行向量与全文混合检索",
			Category:    "检索",
			ToolType:    "builtin",
			Parameters:  string(paramsQueryJSON),
			Metadata:    metaJSON(true, false, false, []string{"knowledge_base"}),
			OwnerName:   "system",
			Visibility:  model.AssetVisibilityPublic,
			Status:      1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			Name:        "search_camera",
			Description: "搜索当前智能体显式绑定的摄像头事件",
			Category:    "检索",
			ToolType:    "builtin",
			Parameters:  string(paramsQueryJSON),
			Metadata:    metaJSON(true, false, false, []string{"camera_event"}),
			OwnerName:   "system",
			Visibility:  model.AssetVisibilityPublic,
			Status:      1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			Name:        "search_videos",
			Description: "搜索当前智能体授权的视频/监控内容",
			Category:    "检索",
			ToolType:    "builtin",
			Parameters:  string(paramsQueryJSON),
			Metadata:    metaJSON(true, false, false, []string{"video_source", "camera_event"}),
			OwnerName:   "system",
			Visibility:  model.AssetVisibilityPublic,
			Status:      1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			Name:        "get_time",
			Description: "获取当前时间",
			Category:    "工具",
			ToolType:    "builtin",
			Parameters:  "{}",
			Metadata:    metaJSON(true, false, false, nil),
			OwnerName:   "system",
			Visibility:  model.AssetVisibilityPublic,
			Status:      1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			Name:        "generate_report",
			Description: "生成分析报告",
			Category:    "工具",
			ToolType:    "builtin",
			Parameters:  string(paramsReportJSON),
			Metadata:    metaJSON(false, true, true, nil),
			OwnerName:   "system",
			Visibility:  model.AssetVisibilityPublic,
			Status:      1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		// ---------- 运维类工具 ----------
		{
			Name:        "list_hosts",
			Description: "列出当前智能体可操作的主机列表",
			Category:    "运维",
			ToolType:    "builtin",
			Parameters:  "{}",
			Metadata:    metaJSON(true, false, false, []string{"host_group"}),
			OwnerName:   "system",
			Visibility:  model.AssetVisibilityPublic,
			Status:      1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			Name:        "exec_command",
			Description: "在指定主机上执行 shell 命令并返回结果",
			Category:    "运维",
			ToolType:    "builtin",
			Parameters:  string(paramsExecJSON),
			Metadata:    metaJSON(false, true, true, []string{"host_group"}),
			OwnerName:   "system",
			Visibility:  model.AssetVisibilityPublic,
			Status:      1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			Name:        "list_dir",
			Description: "列出远程主机上指定目录的文件和子目录",
			Category:    "运维",
			ToolType:    "builtin",
			Parameters:  string(paramsPathJSON),
			Metadata:    metaJSON(true, false, false, []string{"host_group"}),
			OwnerName:   "system",
			Visibility:  model.AssetVisibilityPublic,
			Status:      1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			Name:        "read_file",
			Description: "读取远程主机上的文本文件内容",
			Category:    "运维",
			ToolType:    "builtin",
			Parameters:  string(paramsPathJSON),
			Metadata:    metaJSON(true, false, false, []string{"host_group"}),
			OwnerName:   "system",
			Visibility:  model.AssetVisibilityPublic,
			Status:      1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			Name:        "write_file",
			Description: "写入内容到远程主机的指定文件，可选备份",
			Category:    "运维",
			ToolType:    "builtin",
			Parameters:  string(paramsWriteJSON),
			Metadata:    metaJSON(false, true, true, []string{"host_group"}),
			OwnerName:   "system",
			Visibility:  model.AssetVisibilityPublic,
			Status:      1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			Name:        "write_local_file",
			Description: "将生成内容（如旅行攻略 HTML）写入平台导出目录，并返回可访问的 /api/chat/exports/ 链接（免登录，可直接分享），用于产物落盘与分享",
			Category:    "文件",
			ToolType:    "builtin",
			Parameters:  string(paramsLocalWriteJSON),
			Metadata:    metaJSON(false, true, false, nil),
			OwnerName:   "system",
			Visibility:  model.AssetVisibilityPublic,
			Status:      1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		// ---------- 主机指标 / 只读查询（参考 1Shell host_* 工具） ----------
		{
			Name:        "host_cpu_info",
			Description: "查看远程主机的 CPU 信息（架构、核数、负载），等价于 1Shell 的 host_cpu",
			Category:    "运维",
			ToolType:    "builtin",
			Parameters:  string(paramsExecJSON),
			Metadata:    metaJSON(true, false, false, []string{"host_group"}),
			OwnerName:   "system",
			Visibility:  model.AssetVisibilityPublic,
			Status:      1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			Name:        "host_mem_info",
			Description: "查看远程主机的内存信息（free / /proc/meminfo）",
			Category:    "运维",
			ToolType:    "builtin",
			Parameters:  string(paramsExecJSON),
			Metadata:    metaJSON(true, false, false, []string{"host_group"}),
			OwnerName:   "system",
			Visibility:  model.AssetVisibilityPublic,
			Status:      1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			Name:        "host_disk_info",
			Description: "查看远程主机的磁盘使用与分区信息（df / lsblk）",
			Category:    "运维",
			ToolType:    "builtin",
			Parameters:  string(paramsExecJSON),
			Metadata:    metaJSON(true, false, false, []string{"host_group"}),
			OwnerName:   "system",
			Visibility:  model.AssetVisibilityPublic,
			Status:      1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			Name:        "host_network_info",
			Description: "查看远程主机的网络信息（网卡地址、路由、监听端口）",
			Category:    "运维",
			ToolType:    "builtin",
			Parameters:  string(paramsExecJSON),
			Metadata:    metaJSON(true, false, false, []string{"host_group"}),
			OwnerName:   "system",
			Visibility:  model.AssetVisibilityPublic,
			Status:      1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			Name:        "host_process_list",
			Description: "查看远程主机按 CPU 占用排序的进程列表（ps aux）",
			Category:    "运维",
			ToolType:    "builtin",
			Parameters:  string(paramsExecJSON),
			Metadata:    metaJSON(true, false, false, []string{"host_group"}),
			OwnerName:   "system",
			Visibility:  model.AssetVisibilityPublic,
			Status:      1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			Name:        "host_env",
			Description: "查看远程主机的环境变量（env）",
			Category:    "运维",
			ToolType:    "builtin",
			Parameters:  string(paramsExecJSON),
			Metadata:    metaJSON(true, false, false, []string{"host_group"}),
			OwnerName:   "system",
			Visibility:  model.AssetVisibilityPublic,
			Status:      1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			Name:        "host_service_status",
			Description: "查看远程主机上指定 systemd 服务的状态",
			Category:    "运维",
			ToolType:    "builtin",
			Parameters:  string(paramsServiceJSON),
			Metadata:    metaJSON(true, false, false, []string{"host_group"}),
			OwnerName:   "system",
			Visibility:  model.AssetVisibilityPublic,
			Status:      1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			Name:        "host_probe",
			Description: "从远程主机探测目标地址连通性（ping / tcp / http），等价于 1Shell 的 host_ping/host_telnet",
			Category:    "运维",
			ToolType:    "builtin",
			Parameters:  string(paramsProbeJSON),
			Metadata:    metaJSON(true, false, false, []string{"host_group"}),
			OwnerName:   "system",
			Visibility:  model.AssetVisibilityPublic,
			Status:      1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			Name:        "host_download_file",
			Description: "从远程主机读取文件并以 base64 返回（只读）",
			Category:    "运维",
			ToolType:    "builtin",
			Parameters:  string(paramsDownloadJSON),
			Metadata:    metaJSON(true, false, false, []string{"host_group"}),
			OwnerName:   "system",
			Visibility:  model.AssetVisibilityPublic,
			Status:      1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			Name:        "host_run_script",
			Description: "将脚本写入远程主机并执行（有副作用），等价于 1Shell 的 run_script",
			Category:    "运维",
			ToolType:    "builtin",
			Parameters:  string(paramsScriptJSON),
			Metadata:    metaJSON(false, true, true, []string{"host_group"}),
			OwnerName:   "system",
			Visibility:  model.AssetVisibilityPublic,
			Status:      1,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
	}

	for i := range tools {
		var c int64
		s.db.WithContext(ctx).Model(&model.ToolLibrary{}).Where("name = ?", tools[i].Name).Count(&c)
		if c > 0 {
			continue
		}
		s.db.WithContext(ctx).Create(&tools[i])
	}

	ilog.Infof("seeded tool library items (idempotent)")
}