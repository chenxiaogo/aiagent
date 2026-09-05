// 平台提示词类型（与后端 model.PromptType* 常量对应）
// 提示词库与智能体设置页共用，避免两处各维护一份。
export const PROMPT_TYPES = [
  { value: 'intent-recognition', label: '意图识别' },
  { value: 'query-enhancement', label: '查询增强' },
  { value: 'feasibility-assessment', label: '可行性评估' },
  { value: 'planner', label: '计划生成' },
  { value: 'video-analyze', label: '视频分析' },
  { value: 'scene-describe', label: '场景描述' },
  { value: 'summary', label: '摘要生成' },
  { value: 'report', label: '报告生成' },
  { value: 'chart-selector', label: '图表选择器' },
  { value: 'semantic-consistency', label: '语义一致性' },
  { value: 'answer', label: '问答' },
  { value: 'json-fix', label: 'JSON 修复' },
  { value: 'camera-vision-analysis', label: '摄像头事件分析' },
  { value: 'frame-description', label: '视频帧描述' }
]

export function promptTypeText(type) {
  const hit = PROMPT_TYPES.find(x => x.value === type)
  return hit ? hit.label : type
}
