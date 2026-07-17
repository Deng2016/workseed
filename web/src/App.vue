<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { api } from './api'
import type { Project, Seed, SeedCounts, SeedPriority, SeedStatus, SeedType } from './types'

const types: { value: SeedType | 'all'; label: string; icon: string }[] = [
  { value: 'all', label: '全部类型', icon: '◇' },
  { value: 'idea', label: '灵感', icon: '✦' },
  { value: 'feature', label: '功能', icon: '◈' },
  { value: 'todo', label: '事项', icon: '✓' },
  { value: 'bug', label: 'Bug', icon: '⌁' },
]
const statuses: { value: SeedStatus; label: string }[] = [
  { value: 'inbox', label: '待实现' }, { value: 'done', label: '已完成' },
]
const priorities: { value: SeedPriority; label: string }[] = [
  { value: 'high', label: '高' }, { value: 'middle', label: '中' }, { value: 'low', label: '低' },
]

const projects = ref<Project[]>([])
const seeds = ref<Seed[]>([])
const emptyCounts = (): SeedCounts => ({ total: 0, idea: 0, feature: 0, todo: 0, bug: 0, inbox: 0, done: 0, high: 0, middle: 0, low: 0 })
const counts = ref<SeedCounts>(emptyCounts())
const projectId = ref<number>(0)
const filter = ref<SeedType | 'all'>('all')
const statusFilter = ref<SeedStatus | 'all'>('inbox')
const priorityFilter = ref<SeedPriority | 'all'>('all')
const projectDialog = ref(false)
const seedDialog = ref(false)
const contentInput = ref<HTMLTextAreaElement | null>(null)
const editingId = ref<number | null>(null)
const editingSeed = ref<Seed | null>(null)
const busy = ref(false)
const error = ref('')
const projectForm = reactive({ name: '', description: '' })
const seedForm = reactive({ type: 'todo' as SeedType, status: 'inbox' as SeedStatus, title: '', content: '', priority: 'middle' as SeedPriority })

const currentProject = computed(() => projects.value.find(p => p.id === projectId.value))
const count = (type: SeedType | 'all') => type === 'all' ? counts.value.total : counts.value[type]
const statusCount = (status: SeedStatus | 'all') => status === 'all' ? counts.value.total : counts.value[status]
const priorityCount = (priority: SeedPriority | 'all') => priority === 'all' ? counts.value.total : counts.value[priority]

async function loadProjects() {
  try {
    projects.value = await api.projects()
    if (!projectId.value && projects.value.length) projectId.value = projects.value[0].id
    if (!projects.value.length) projectDialog.value = true
  } catch (e) { showError(e) }
}
async function loadSeeds() {
  if (!projectId.value) { seeds.value = []; counts.value = emptyCounts(); return }
  busy.value = true
  try {
    const result = await api.seeds(projectId.value, filter.value, statusFilter.value, priorityFilter.value)
    seeds.value = result.items
    counts.value = result.counts
  } catch (e) { showError(e) }
  finally { busy.value = false }
}
async function createProject() {
  if (!projectForm.name.trim()) return
  try {
    const item = await api.createProject(projectForm)
    projects.value.unshift(item); projectId.value = item.id
    projectForm.name = ''; projectForm.description = ''; projectDialog.value = false
  } catch (e) { showError(e) }
}
function openSeed(seed?: Seed) {
  editingId.value = seed?.id ?? null
  editingSeed.value = seed ?? null
  seedForm.type = seed?.type ?? (filter.value === 'all' ? 'todo' : filter.value)
  seedForm.status = seed?.status ?? 'inbox'
  seedForm.title = seed?.title ?? ''
  seedForm.content = seed?.content ?? ''
  seedForm.priority = seed?.priority ?? 'middle'
  seedDialog.value = true
  nextTick(resizeContent)
}
function resizeContent(target: HTMLTextAreaElement | null = contentInput.value) {
  if (!target) return
  target.style.height = 'auto'
  target.style.height = `${target.scrollHeight}px`
}
function resizeContentFromEvent(event: Event) {
  resizeContent(event.target as HTMLTextAreaElement)
}
async function saveSeed() {
  if (!seedForm.title.trim() || !projectId.value) return
  const value = { id: editingId.value ?? 0, projectId: projectId.value, ...seedForm }
  try {
    if (editingId.value) await api.updateSeed(value)
    else await api.createSeed(value)
    seedDialog.value = false; await loadSeeds()
  } catch (e) { showError(e) }
}
async function updateSeedMeta(seed: Seed, field: "type" | "status" | "priority", event: Event) {
  const value = (event.target as HTMLSelectElement).value as SeedType | SeedStatus | SeedPriority
  const previous = seed[field]
  Object.assign(seed, { [field]: value })
  try {
    await api.updateSeed(seed)
    await loadSeeds()
  } catch (e) {
    Object.assign(seed, { [field]: previous })
    showError(e)
  }
}
async function removeSeed(id: number) {
  if (!confirm('确定删除这颗种子吗？')) return
  try { await api.deleteSeed(id); await loadSeeds() } catch (e) { showError(e) }
}
function showError(value: unknown) { error.value = value instanceof Error ? value.message : '操作失败'; window.setTimeout(() => error.value = '', 3000) }
function typeInfo(value: SeedType) { return types.find(t => t.value === value)! }
function statusLabel(value: SeedStatus) { return statuses.find(s => s.value === value)?.label }
function formatTime(value?: string | null) {
  if (!value) return '尚未完成'
  const normalized = value.includes('T') ? value : value.replace(' ', 'T') + 'Z'
  const date = new Date(normalized)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false })
}

function handleKeydown(event: KeyboardEvent) {
  if (event.key !== 'Escape') return
  if (seedDialog.value) seedDialog.value = false
  else if (projectDialog.value) projectDialog.value = false
}

watch([projectId, filter, statusFilter, priorityFilter], loadSeeds)
onMounted(() => {
  document.addEventListener('keydown', handleKeydown)
  loadProjects()
})
onBeforeUnmount(() => document.removeEventListener('keydown', handleKeydown))
</script>

<template>
  <main class="shell">
    <header class="topbar">
      <div class="brand"><img class="brand-mark" src="/favicon.png" alt="" /><div><strong>拾种</strong><small>WORKSEED</small></div><span class="brand-tagline">把工作中的每个念头，都安放在这里。</span></div>
      <div class="project-picker">
        <span class="label">当前项目</span>
        <select v-model="projectId"><option v-for="p in projects" :key="p.id" :value="p.id">{{ p.name }}</option></select>
        <button class="quiet" @click="projectDialog = true">＋ 新建项目</button>
      </div>
      <button class="primary" :disabled="!projectId" @click="openSeed()">＋ 播下一颗种子</button>
    </header>

    <section v-if="currentProject?.description" class="hero">
      <p class="eyebrow">{{ currentProject.description }}</p>
    </section>

    <div class="filter-row">
      <label class="filter-control">类型
        <select v-model="filter" aria-label="按种子类型筛选">
          <option value="all">全部类型（{{ count('all') }}）</option>
          <option v-for="item in types.slice(1)" :key="item.value" :value="item.value">{{ item.label }}（{{ count(item.value) }}）</option>
        </select>
      </label>
      <label class="filter-control">状态
        <select v-model="statusFilter" aria-label="按种子状态筛选">
          <option value="all">全部状态（{{ statusCount('all') }}）</option>
          <option v-for="item in statuses" :key="item.value" :value="item.value">{{ item.label }}（{{ statusCount(item.value) }}）</option>
        </select>
      </label>
      <label class="filter-control">优先级
        <select v-model="priorityFilter" aria-label="按种子优先级筛选">
          <option value="all">全部优先级（{{ priorityCount('all') }}）</option>
          <option v-for="item in priorities" :key="item.value" :value="item.value">{{ item.label }}（{{ priorityCount(item.value) }}）</option>
        </select>
      </label>
      <div class="filter-result" aria-live="polite">当前共过滤出 <strong>{{ seeds.length }}</strong> 颗种子</div>
    </div>

    <section class="seed-list">
      <p v-if="busy" class="empty">正在翻土……</p>
      <div v-else-if="!projectId" class="empty"><b>先创建一个项目</b><span>项目是一片苗圃，用来收纳相关的工作种子。</span></div>
      <div v-else-if="!seeds.length && (filter !== 'all' || statusFilter !== 'all' || priorityFilter !== 'all')" class="empty"><b>没有符合条件的种子</b><span>尝试切换类型、状态或优先级筛选。</span><button class="quiet" v-on:click="filter = 'all'; statusFilter = 'all'; priorityFilter = 'all'">清除筛选</button></div>
      <div v-else-if="!seeds.length" class="empty"><b>这里还没有种子</b><span>记录一闪而过的灵感，或下一件要完成的事。</span><button class="primary" v-on:click="openSeed()">播下第一颗</button></div>
      <article v-for="seed in seeds" :key="seed.id" class="seed-card" @click="openSeed(seed)">
        <div class="type-dot" :class="seed.type">{{ typeInfo(seed.type).icon }}</div>
        <div class="seed-body"><div class="seed-meta"><select :value="seed.type" aria-label="种子类型" @click.stop @change="updateSeedMeta(seed, 'type', $event)"><option v-for="item in types.slice(1)" :key="item.value" :value="item.value">{{ item.label }}</option></select><i>·</i><select :value="seed.status" aria-label="种子状态" @click.stop @change="updateSeedMeta(seed, 'status', $event)"><option v-for="item in statuses" :key="item.value" :value="item.value">{{ item.label }}</option></select><i>·</i><select :value="seed.priority" aria-label="种子优先级" @click.stop @change="updateSeedMeta(seed, 'priority', $event)"><option v-for="item in priorities" :key="item.value" :value="item.value">{{ item.label }}</option></select></div><div class="seed-main"><h2>{{ seed.title }}</h2><p v-if="seed.content">{{ seed.content }}</p></div></div>
        <button class="icon-button" title="删除" @click.stop="removeSeed(seed.id)">×</button>
      </article>
    </section>
  </main>

  <div v-if="projectDialog" class="overlay" @click.self="projectDialog = false">
    <form class="dialog" @submit.prevent="createProject"><button type="button" class="close" @click="projectDialog = false">×</button><p class="eyebrow">新的苗圃</p><h2>创建项目</h2><label>项目名称<input v-model="projectForm.name" autofocus placeholder="例如：Workseed" /></label><label>简单描述<textarea v-model="projectForm.description" rows="3" placeholder="这个项目在做什么？"></textarea></label><div class="actions"><button type="button" class="quiet" @click="projectDialog = false">取消</button><button class="primary">创建项目</button></div></form>
  </div>

  <div v-if="seedDialog" class="overlay" @click.self="seedDialog = false">
    <form class="dialog seed-form" @submit.prevent="saveSeed"><button type="button" class="close" @click="seedDialog = false">×</button><p class="eyebrow">{{ editingId ? '照料种子' : '捕捉此刻' }}</p><h2>{{ editingId ? '编辑种子' : '播下一颗种子' }}</h2><div class="grid"><label>类型<select v-model="seedForm.type"><option v-for="item in types.slice(1)" :key="item.value" :value="item.value">{{ item.label }}</option></select></label><label>状态<select v-model="seedForm.status"><option v-for="item in statuses" :key="item.value" :value="item.value">{{ item.label }}</option></select></label><label>优先级<select v-model="seedForm.priority"><option v-for="item in priorities" :key="item.value" :value="item.value">{{ item.label }}</option></select></label></div><label>标题<input v-model="seedForm.title" autofocus placeholder="一句话记下它" /></label><label>详细内容<textarea ref="contentInput" v-model="seedForm.content" rows="9" v-on:input="resizeContentFromEvent" placeholder="背景、想法或验收方式……"></textarea></label><div v-if="editingSeed" class="seed-timestamps"><span>创建时间<strong>{{ formatTime(editingSeed.createdAt) }}</strong></span><span>完成时间<strong>{{ formatTime(editingSeed.completedAt) }}</strong></span></div><div class="actions"><button type="button" class="quiet" @click="seedDialog = false">取消</button><button class="primary">{{ editingId ? '保存修改' : '播下种子' }}</button></div></form>
  </div>
  <div v-if="error" class="toast">{{ error }}</div>
</template>

