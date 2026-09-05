<template>
  <div>
    <div class="app-card section">
      <ModelBinding ref="modelRef" :agent-id="agentId" />
    </div>

    <div class="action-bar">
      <el-button type="primary" :loading="saving" @click="handleSave">保存模型配置</el-button>
      <span class="tip">一个智能体可绑定多个模型，按用途路由、同用途内按优先级回退</span>
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import ModelBinding from '@/components/agent/ModelBinding.vue'

const route = useRoute()
const agentId = computed(() => Number(route.params.id) || 0)

const saving = ref(false)
const modelRef = ref(null)

async function handleSave() {
  saving.value = true
  try {
    await modelRef.value?.save(agentId.value)
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.section { padding: 20px 24px; margin-bottom: 16px; }
.action-bar { display: flex; align-items: center; }
.tip { font-size: 12px; color: var(--text-muted); margin-left: 12px; }
</style>
