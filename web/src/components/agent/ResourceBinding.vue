<template>
  <div v-loading="loading">
    <div class="section-head">
      <div>
        <div class="card-title">运行资源边界</div>
        <div class="tip">Agent 只能检索显式绑定的资源；未绑定时不会回退到全平台数据。</div>
      </div>
      <el-button type="primary" :loading="saving" @click="save">保存资源</el-button>
    </div>
    <el-form label-width="110px" class="resource-form">
      <el-form-item label="知识库">
        <el-select v-model="form.knowledgeBaseIds" multiple filterable collapse-tags placeholder="选择知识库" style="width:100%">
          <el-option v-for="item in data.availableKnowledgeBases" :key="item.id" :label="item.name" :value="item.id" />
        </el-select>
        <span class="tip inline">知识库是统一的内容容器（视频 / 摄像头 / 文件各成一类），绑定后即拥有其全部检索权限。</span>
      </el-form-item>
    </el-form>
  </div>
</template>

<script setup>
import { reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { getAgentResources, saveAgentResources } from '@/api/agent'

const props = defineProps({ agentId: { type: Number, required: true } })
const loading = ref(false)
const saving = ref(false)
const data = reactive({
  availableKnowledgeBases: []
})
const form = reactive({
  knowledgeBaseIds: []
})

async function load() {
  if (!props.agentId) return
  loading.value = true
  try {
    const res = await getAgentResources(props.agentId)
    if (res.code !== 0) return
    const value = res.data || {}
    data.availableKnowledgeBases = value.availableKnowledgeBases || []
    form.knowledgeBaseIds = (value.knowledgeBases || []).map(item => item.id)
  } finally { loading.value = false }
}

async function save() {
  saving.value = true
  try {
    const res = await saveAgentResources(props.agentId, { ...form })
    if (res.code === 0) ElMessage.success('资源边界已保存')
    else ElMessage.error(res.message || '保存失败')
  } finally { saving.value = false }
}

watch(() => props.agentId, load, { immediate: true })
</script>

<style scoped>
.section-head { display:flex; align-items:flex-start; justify-content:space-between; gap:16px; margin-bottom:16px; }
.card-title { font-size:15px; font-weight:600; color:var(--text); }
.tip { margin-top:4px; font-size:12px; color:var(--text-muted); }
.tip.inline { margin-left:10px; }
.resource-form { max-width:760px; }
</style>
