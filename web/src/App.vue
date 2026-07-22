<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { api } from './api'
import type { Project, Seed, SeedCounts, SeedPriority, SeedStatus, SeedType } from './types'

type Page = 'seeds' | 'worklogs'
type QuickRange = 'year' | 'month' | 'week' | 'today'
interface WorklogDay { key: string; label: string; items: Seed[] }
interface WorklogMonth { key: string; label: string; days: WorklogDay[]; count: number }
interface WorklogYear { key: string; label: string; months: WorklogMonth[]; count: number }

const types: { value: SeedType | 'all'; label: string; icon: string }[] = [
  { value: 'all', label: '全部类型', icon: '◇' },
  { value: 'idea', label: '灵感', icon: '✦' },
  { value: 'feature', label: '功能', icon: '◈' },
  { value: 'todo', label: '事项', icon: '✓' },
  { value: 'bug', label: 'Bug', icon: '⌁' },
]
const statuses: { value: SeedStatus; label: string }[] = [
  { value: 'inbox', label: '待实现' }, { value: 'doing', label: '进行中' }, { value: 'done', label: '已完成' },
]
const priorities: { value: SeedPriority; label: string }[] = [
  { value: 'high', label: '高' }, { value: 'middle', label: '中' }, { value: 'low', label: '低' },
]

const projects = ref<Project[]>([])
const seeds = ref<Seed[]>([])
const appVersion = ref('')
const currentPage = ref<Page>(window.location.hash === '#/worklogs' ? 'worklogs' : 'seeds')
const worklogs = ref<Seed[]>([])
const worklogBusy = ref(false)
const collapsedNodes = ref(new Set<string>())
const initialWorklogRange = quickRangeDates('month')
const worklogRange = reactive({ start: initialWorklogRange.start, end: initialWorklogRange.end })
const activeQuickRange = ref<QuickRange | null>('month')
const emptyCounts = (): SeedCounts => ({ total: 0, idea: 0, feature: 0, todo: 0, bug: 0, inbox: 0, doing: 0, done: 0, high: 0, middle: 0, low: 0 })
const counts = ref<SeedCounts>(emptyCounts())
const projectId = ref<number>(0)
const filter = ref<SeedType[]>(['idea', 'feature', 'todo', 'bug'])
const statusFilter = ref<SeedStatus[]>(['inbox', 'doing'])
const priorityFilter = ref<SeedPriority[]>(['high', 'middle', 'low'])
const projectDialog = ref(false)
const seedDialog = ref(false)
const contentInput = ref<HTMLTextAreaElement | null>(null)
const editingId = ref<number | null>(null)
const editingSeed = ref<Seed | null>(null)
const copiedSeedId = ref<number | null>(null)
const busy = ref(false)
const loadingMore = ref(false)
const seedPage = ref(0)
const filteredTotal = ref(0)
const hasMoreSeeds = ref(false)
const loadMoreTrigger = ref<HTMLElement | null>(null)
const error = ref('')
let copyFeedbackTimer: number | undefined
let seedLoadToken = 0
let loadMoreObserver: IntersectionObserver | undefined
const seedPageSize = 20
const projectForm = reactive({ name: '', description: '' })
const seedForm = reactive({ type: 'todo' as SeedType, status: 'inbox' as SeedStatus, title: '', content: '', priority: 'middle' as SeedPriority })

const currentProject = computed(() => projects.value.find(p => p.id === projectId.value))
const count = (type: SeedType | 'all') => type === 'all' ? counts.value.total : counts.value[type]
const statusCount = (status: SeedStatus | 'all') => status === 'all' ? counts.value.total : counts.value[status]
const priorityCount = (priority: SeedPriority | 'all') => priority === 'all' ? counts.value.total : counts.value[priority]
const worklogGroups = computed<WorklogYear[]>(() => {
  const years = new Map<string, { label: string; months: Map<string, { label: string; days: Map<string, WorklogDay> }> }>()
  for (const item of worklogs.value) {
    const date = parseStoredTime(item.completedAt)
    if (!date) continue
    const yearKey = String(date.getFullYear())
    const monthNumber = date.getMonth() + 1
    const monthKey = `${yearKey}-${String(monthNumber).padStart(2, '0')}`
    const dayKey = `${monthKey}-${String(date.getDate()).padStart(2, '0')}`
    if (!years.has(yearKey)) years.set(yearKey, { label: `${yearKey}年`, months: new Map() })
    const year = years.get(yearKey)!
    if (!year.months.has(monthKey)) year.months.set(monthKey, { label: `${monthNumber}月`, days: new Map() })
    const month = year.months.get(monthKey)!
    if (!month.days.has(dayKey)) month.days.set(dayKey, { key: dayKey, label: `${date.getDate()}日`, items: [] })
    month.days.get(dayKey)!.items.push(item)
  }
  return Array.from(years, ([yearKey, year]) => {
    const months = Array.from(year.months, ([monthKey, month]) => {
      const days = Array.from(month.days.values())
      return { key: monthKey, label: month.label, days, count: days.reduce((sum, day) => sum + day.items.length, 0) }
    })
    return { key: yearKey, label: year.label, months, count: months.reduce((sum, month) => sum + month.count, 0) }
  })
})

async function loadProjects() {
  try {
    projects.value = await api.projects()
    if (!projectId.value && projects.value.length) projectId.value = projects.value[0].id
    if (!projects.value.length && currentPage.value === 'seeds') projectDialog.value = true
  } catch (e) { showError(e) }
}
async function loadVersion() {
  try { appVersion.value = (await api.version()).version } catch { /* 版本信息不影响主功能 */ }
}
async function loadSeeds(reset = true) {
  if (currentPage.value !== 'seeds') return
  if (!projectId.value) {
    seedLoadToken++
    seeds.value = []; counts.value = emptyCounts(); seedPage.value = 0; filteredTotal.value = 0; hasMoreSeeds.value = false
    busy.value = false; loadingMore.value = false
    return
  }
  if (!reset && (busy.value || loadingMore.value || !hasMoreSeeds.value)) return
  const token = reset ? ++seedLoadToken : seedLoadToken
  const nextPage = reset ? 1 : seedPage.value + 1
  if (reset) {
    busy.value = true
    loadingMore.value = false
    seeds.value = []
    seedPage.value = 0
    filteredTotal.value = 0
    hasMoreSeeds.value = false
    counts.value = emptyCounts()
  } else loadingMore.value = true
  let loadedSuccessfully = false
  try {
    const result = await api.seeds(projectId.value, [...filter.value], [...statusFilter.value], [...priorityFilter.value], nextPage, seedPageSize)
    if (token !== seedLoadToken) return
    if (reset) seeds.value = result.items
    else {
      const loadedIds = new Set(seeds.value.map(seed => seed.id))
      seeds.value.push(...result.items.filter(seed => !loadedIds.has(seed.id)))
    }
    counts.value = result.counts
    seedPage.value = result.page
    filteredTotal.value = result.total
    hasMoreSeeds.value = result.hasMore
    loadedSuccessfully = true
  } catch (e) {
    if (token === seedLoadToken) showError(e)
  } finally {
    if (token === seedLoadToken) {
      busy.value = false
      loadingMore.value = false
      await nextTick()
      if (loadedSuccessfully && loadMoreTrigger.value) {
        loadMoreObserver?.unobserve(loadMoreTrigger.value)
        loadMoreObserver?.observe(loadMoreTrigger.value)
      }
    }
  }
}
function loadMoreSeeds() { loadSeeds(false) }
async function loadWorklogs() {
  if (!worklogRange.start || !worklogRange.end) return
  const start = new Date(`${worklogRange.start}T00:00:00`)
  const end = new Date(`${worklogRange.end}T00:00:00`)
  if (start > end) { showError('开始时间不能晚于结束时间'); return }
  end.setDate(end.getDate() + 1)
  worklogBusy.value = true
  try {
    worklogs.value = await api.worklogs(start.toISOString(), end.toISOString())
    collapsedNodes.value = new Set()
  } catch (e) { showError(e) }
  finally { worklogBusy.value = false }
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
  seedForm.type = seed?.type ?? 'todo'
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
async function writeClipboard(text: string) {
  if (navigator.clipboard?.writeText) {
    try { await navigator.clipboard.writeText(text); return } catch { /* 使用兼容方案 */ }
  }
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.style.cssText = 'position:fixed;opacity:0;pointer-events:none'
  document.body.appendChild(textarea)
  textarea.select()
  const copied = document.execCommand('copy')
  textarea.remove()
  if (!copied) throw new Error('复制失败，请检查浏览器剪贴板权限')
}
async function copySeed(seed: Seed) {
  const text = [seed.title, seed.content].filter(Boolean).join('\n')
  try {
    await writeClipboard(text)
    copiedSeedId.value = seed.id
    window.clearTimeout(copyFeedbackTimer)
    copyFeedbackTimer = window.setTimeout(() => copiedSeedId.value = null, 1600)
  } catch (e) { showError(e) }
}
async function removeSeed(id: number) {
  if (!confirm('确定删除这颗种子吗？')) return
  try { await api.deleteSeed(id); await loadSeeds() } catch (e) { showError(e) }
}
function showError(value: unknown) { error.value = value instanceof Error ? value.message : '操作失败'; window.setTimeout(() => error.value = '', 3000) }
function typeInfo(value: SeedType) { return types.find(t => t.value === value)! }
function statusLabel(value: SeedStatus) { return statuses.find(s => s.value === value)?.label }
function projectName(id: number) { return projects.value.find(project => project.id === id)?.name ?? '未知项目' }
function parseStoredTime(value?: string | null) {
  if (!value) return null
  const normalized = value.includes('T') ? value : value.replace(' ', 'T') + 'Z'
  const date = new Date(normalized)
  return Number.isNaN(date.getTime()) ? null : date
}
function formatTime(value?: string | null) {
  if (!value) return '—'
  const date = parseStoredTime(value)
  return date ? date.toLocaleString('zh-CN', { hour12: false }) : value
}
function formatDuration(value?: number | null) {
  if (value == null) return '—'
  const days = Math.floor(value / 86400)
  const hours = Math.floor(value % 86400 / 3600)
  const minutes = Math.floor(value % 3600 / 60)
  const seconds = value % 60
  const parts = []
  if (days) parts.push(`${days}天`)
  if (hours) parts.push(`${hours}小时`)
  if (minutes) parts.push(`${minutes}分钟`)
  if (!parts.length || seconds) parts.push(`${seconds}秒`)
  return parts.join(' ')
}
function formatDateInput(date: Date) {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}
function quickRangeDates(range: QuickRange) {
  const now = new Date()
  let start = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  let end = new Date(start)
  if (range === 'year') {
    start = new Date(now.getFullYear(), 0, 1)
    end = new Date(now.getFullYear(), 11, 31)
  } else if (range === 'month') {
    start = new Date(now.getFullYear(), now.getMonth(), 1)
    end = new Date(now.getFullYear(), now.getMonth() + 1, 0)
  } else if (range === 'week') {
    const offsetFromMonday = (now.getDay() + 6) % 7
    start.setDate(start.getDate() - offsetFromMonday)
    end = new Date(start)
    end.setDate(end.getDate() + 6)
  }
  return { start: formatDateInput(start), end: formatDateInput(end) }
}
function applyQuickRange(range: QuickRange) {
  Object.assign(worklogRange, quickRangeDates(range))
  activeQuickRange.value = range
  loadWorklogs()
}
function useCustomRange() { activeQuickRange.value = null }
function resetFilters() {
  filter.value = ['idea', 'feature', 'todo', 'bug']
  statusFilter.value = ['inbox', 'doing']
  priorityFilter.value = ['high', 'middle', 'low']
}
function toggleSelection<T>(values: T[], value: T) {
  const index = values.indexOf(value)
  if (index === -1) values.push(value)
  else values.splice(index, 1)
}
function toggleTypeFilter(value: SeedType | 'all') {
  if (value !== 'all') toggleSelection(filter.value, value)
}
function isTypeSelected(value: SeedType | 'all') { return value !== 'all' && filter.value.includes(value) }
function toggleStatusFilter(value: SeedStatus) { toggleSelection(statusFilter.value, value) }
function togglePriorityFilter(value: SeedPriority) { toggleSelection(priorityFilter.value, value) }
function closeFilterDropdown(event: MouseEvent) {
  const details = event.currentTarget as HTMLDetailsElement
  details.open = false
}
function toggleNode(key: string) {
  const next = new Set(collapsedNodes.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  collapsedNodes.value = next
}
function isCollapsed(key: string) { return collapsedNodes.value.has(key) }
function syncPageFromHash() {
  currentPage.value = window.location.hash === '#/worklogs' ? 'worklogs' : 'seeds'
  projectDialog.value = false
  seedDialog.value = false
  if (currentPage.value === 'worklogs') loadWorklogs()
  else if (projectId.value) loadSeeds()
  else if (!projects.value.length) projectDialog.value = true
}

function handleKeydown(event: KeyboardEvent) {
  if (event.key !== 'Escape') return
  if (seedDialog.value) seedDialog.value = false
  else if (projectDialog.value) projectDialog.value = false
}

watch([projectId, filter, statusFilter, priorityFilter], () => loadSeeds(), { deep: true })
watch(loadMoreTrigger, (current, previous) => {
  if (previous) loadMoreObserver?.unobserve(previous)
  if (current) loadMoreObserver?.observe(current)
})
onMounted(() => {
  document.addEventListener('keydown', handleKeydown)
  window.addEventListener('hashchange', syncPageFromHash)
  loadMoreObserver = new IntersectionObserver(entries => {
    if (entries.some(entry => entry.isIntersecting)) loadMoreSeeds()
  }, { rootMargin: '240px 0px' })
  if (loadMoreTrigger.value) loadMoreObserver.observe(loadMoreTrigger.value)
  loadVersion()
  loadProjects()
  if (currentPage.value === 'worklogs') loadWorklogs()
})
onBeforeUnmount(() => {
  document.removeEventListener('keydown', handleKeydown)
  window.removeEventListener('hashchange', syncPageFromHash)
  loadMoreObserver?.disconnect()
  window.clearTimeout(copyFeedbackTimer)
})
</script>

<template>
  <main v-if="currentPage === 'seeds'" class="shell">
    <header class="topbar">
      <div class="brand"><a class="brand-link" href="#/worklogs" title="查看工作日志"><img class="brand-mark" src="/favicon.png" alt="" /><span><strong>拾种</strong><small>WORKSEED</small></span></a><span class="brand-tagline">把工作中的每个念头，都安放在这里。</span></div>
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
      <details class="filter-control filter-dropdown" @mouseleave="closeFilterDropdown">
        <summary><span>类型</span><strong>已选 {{ filter.length }}/{{ types.length - 1 }}</strong></summary>
        <div class="filter-menu" role="group" aria-label="按种子类型筛选"><label v-for="item in types.slice(1)" :key="item.value" class="check-option" @click.prevent="toggleTypeFilter(item.value)"><input type="checkbox" :checked="isTypeSelected(item.value)" /><span>{{ item.label }}</span><em>{{ count(item.value) }}</em></label></div>
      </details>
      <details class="filter-control filter-dropdown" @mouseleave="closeFilterDropdown">
        <summary><span>状态</span><strong>已选 {{ statusFilter.length }}/{{ statuses.length }}</strong></summary>
        <div class="filter-menu" role="group" aria-label="按种子状态筛选"><label v-for="item in statuses" :key="item.value" class="check-option" @click.prevent="toggleStatusFilter(item.value)"><input type="checkbox" :checked="statusFilter.includes(item.value)" /><span>{{ item.label }}</span><em>{{ statusCount(item.value) }}</em></label></div>
      </details>
      <details class="filter-control filter-dropdown" @mouseleave="closeFilterDropdown">
        <summary><span>优先级</span><strong>已选 {{ priorityFilter.length }}/{{ priorities.length }}</strong></summary>
        <div class="filter-menu" role="group" aria-label="按种子优先级筛选"><label v-for="item in priorities" :key="item.value" class="check-option" @click.prevent="togglePriorityFilter(item.value)"><input type="checkbox" :checked="priorityFilter.includes(item.value)" /><span>{{ item.label }}</span><em>{{ priorityCount(item.value) }}</em></label></div>
      </details>
      <div class="filter-result" aria-live="polite">已显示 <strong>{{ seeds.length }}</strong> / {{ filteredTotal }} 颗种子</div>
    </div>

    <section class="seed-list">
      <p v-if="busy" class="empty">正在翻土……</p>
      <div v-else-if="!projectId" class="empty"><b>先创建一个项目</b><span>项目是一片苗圃，用来收纳相关的工作种子。</span></div>
      <div v-else-if="!seeds.length && counts.total > 0" class="empty"><b>没有符合条件的种子</b><span>尝试切换类型、状态或优先级筛选。</span><button class="quiet" @click="resetFilters">恢复默认筛选</button></div>
      <div v-else-if="!seeds.length" class="empty"><b>这里还没有种子</b><span>记录一闪而过的灵感，或下一件要完成的事。</span><button class="primary" v-on:click="openSeed()">播下第一颗</button></div>
      <article v-for="seed in seeds" :key="seed.id" class="seed-card" @click="openSeed(seed)">
        <div class="type-dot" :class="seed.type">{{ typeInfo(seed.type).icon }}</div>
        <div class="seed-body">
          <div class="seed-meta">
            <select :value="seed.type" aria-label="种子类型" @click.stop @change="updateSeedMeta(seed, 'type', $event)"><option v-for="item in types.slice(1)" :key="item.value" :value="item.value">{{ item.label }}</option></select><i>·</i>
            <select :value="seed.status" aria-label="种子状态" @click.stop @change="updateSeedMeta(seed, 'status', $event)"><option v-for="item in statuses" :key="item.value" :value="item.value">{{ item.label }}</option></select><i>·</i>
            <select :value="seed.priority" aria-label="种子优先级" @click.stop @change="updateSeedMeta(seed, 'priority', $event)"><option v-for="item in priorities" :key="item.value" :value="item.value">{{ item.label }}</option></select>
            <button class="copy-button" type="button" :title="copiedSeedId === seed.id ? '已复制' : '复制标题与内容'" @click.stop="copySeed(seed)">{{ copiedSeedId === seed.id ? '✓ 已复制' : '复制' }}</button>
            <span
              v-if="seed.status === 'done' && (seed.startedAt || seed.completedAt || seed.durationSeconds != null)"
              class="list-timestamps"
            >
              <span v-if="seed.startedAt" class="list-timestamp">开始时间 {{ formatTime(seed.startedAt) }}</span>
              <span v-if="seed.completedAt" class="list-timestamp">完成时间 {{ formatTime(seed.completedAt) }}</span>
              <span v-if="seed.durationSeconds != null" class="list-timestamp">耗时 {{ formatDuration(seed.durationSeconds) }}</span>
            </span>
          </div>
          <div class="seed-main"><h2>{{ seed.title }}</h2><p v-if="seed.content">{{ seed.content }}</p></div>
        </div>
        <button class="icon-button" title="删除" @click.stop="removeSeed(seed.id)">×</button>
      </article>
      <div v-if="seeds.length" ref="loadMoreTrigger" class="load-more-status" aria-live="polite">
        <span v-if="loadingMore">正在加载更多种子……</span>
        <span v-else-if="hasMoreSeeds">继续向下滚动加载更多</span>
        <span v-else>已显示全部 {{ filteredTotal }} 颗种子</span>
      </div>
    </section>
    <footer v-if="appVersion" class="app-version" title="当前版本">版本 {{ appVersion }}</footer>
  </main>

  <main v-else class="shell worklog-page">
    <header class="topbar">
      <div class="brand"><a class="brand-link" href="#/worklogs"><img class="brand-mark" src="/favicon.png" alt="" /><span><strong>拾种</strong><small>WORKSEED</small></span></a><span class="brand-tagline">工作日志</span></div>
      <a class="quiet nav-link" href="#/">返回种子列表</a>
    </header>

    <section class="worklog-shell">
      <div class="worklog-heading">
        <div><p class="eyebrow">WORK JOURNAL</p><h1>工作日志</h1></div>
        <p>按完成时间回看已经落地的工作。</p>
      </div>

      <form class="worklog-filters" @submit.prevent="loadWorklogs">
        <label>开始时间<input v-model="worklogRange.start" type="date" required @input="useCustomRange" /></label>
        <span class="range-separator">—</span>
        <label>结束时间<input v-model="worklogRange.end" type="date" required @input="useCustomRange" /></label>
        <div class="quick-ranges" aria-label="快捷查询">
          <button type="button" :class="{ active: activeQuickRange === 'year' }" @click="applyQuickRange('year')">本年</button>
          <button type="button" :class="{ active: activeQuickRange === 'month' }" @click="applyQuickRange('month')">本月</button>
          <button type="button" :class="{ active: activeQuickRange === 'week' }" @click="applyQuickRange('week')">本周</button>
          <button type="button" :class="{ active: activeQuickRange === 'today' }" @click="applyQuickRange('today')">当天</button>
        </div>
        <button class="primary worklog-search" :disabled="worklogBusy">查询</button>
      </form>

      <div class="worklog-summary" aria-live="polite">共找到 <strong>{{ worklogs.length }}</strong> 条已完成工作</div>
      <p v-if="worklogBusy" class="empty">正在整理工作日志……</p>
      <div v-else-if="!worklogs.length" class="empty"><b>这段时间还没有工作日志</b><span>完成一颗种子后，它会出现在这里。</span></div>
      <section v-else class="worklog-tree">
        <div v-for="year in worklogGroups" :key="year.key" class="worklog-year">
          <button class="tree-node year-node" type="button" :aria-expanded="!isCollapsed(year.key)" @click="toggleNode(year.key)"><span class="tree-arrow" :class="{ collapsed: isCollapsed(year.key) }">⌄</span><strong>{{ year.label }}</strong><em>{{ year.count }} 项</em></button>
          <div v-if="!isCollapsed(year.key)" class="year-children">
            <div v-for="month in year.months" :key="month.key" class="worklog-month">
              <button class="tree-node month-node" type="button" :aria-expanded="!isCollapsed(month.key)" @click="toggleNode(month.key)"><span class="tree-arrow" :class="{ collapsed: isCollapsed(month.key) }">⌄</span><strong>{{ month.label }}</strong><em>{{ month.count }} 项</em></button>
              <div v-if="!isCollapsed(month.key)" class="month-children">
                <div v-for="day in month.days" :key="day.key" class="worklog-day">
                  <button class="tree-node day-node" type="button" :aria-expanded="!isCollapsed(day.key)" @click="toggleNode(day.key)"><span class="tree-arrow" :class="{ collapsed: isCollapsed(day.key) }">⌄</span><strong>{{ day.label }}</strong><em>{{ day.items.length }} 项</em></button>
                  <div v-if="!isCollapsed(day.key)" class="day-children">
                    <article v-for="item in day.items" :key="item.id" class="worklog-item">
                      <div class="worklog-item-meta"><span>{{ projectName(item.projectId) }}</span><span>{{ typeInfo(item.type).label }}</span><time>{{ formatTime(item.completedAt) }}</time><span v-if="item.durationSeconds != null">耗时 {{ formatDuration(item.durationSeconds) }}</span></div>
                      <h2>{{ item.title }}</h2>
                    </article>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>
    </section>
  </main>

  <div v-if="currentPage === 'seeds' && projectDialog" class="overlay" @click.self="projectDialog = false">
    <form class="dialog" @submit.prevent="createProject"><button type="button" class="close" @click="projectDialog = false">×</button><p class="eyebrow">新的苗圃</p><h2>创建项目</h2><label>项目名称<input v-model="projectForm.name" autofocus placeholder="例如：Workseed" /></label><label>简单描述<textarea v-model="projectForm.description" rows="3" placeholder="这个项目在做什么？"></textarea></label><div class="actions"><button type="button" class="quiet" @click="projectDialog = false">取消</button><button class="primary">创建项目</button></div></form>
  </div>

  <div v-if="currentPage === 'seeds' && seedDialog" class="overlay" @click.self="seedDialog = false">
    <form class="dialog seed-form" @submit.prevent="saveSeed"><button type="button" class="close" @click="seedDialog = false">×</button><p class="eyebrow">{{ editingId ? '照料种子' : '捕捉此刻' }}</p><h2>{{ editingId ? '编辑种子' : '播下一颗种子' }}</h2><div class="grid"><label>类型<select v-model="seedForm.type"><option v-for="item in types.slice(1)" :key="item.value" :value="item.value">{{ item.label }}</option></select></label><label>状态<select v-model="seedForm.status"><option v-for="item in statuses" :key="item.value" :value="item.value">{{ item.label }}</option></select></label><label>优先级<select v-model="seedForm.priority"><option v-for="item in priorities" :key="item.value" :value="item.value">{{ item.label }}</option></select></label></div><label>标题<input v-model="seedForm.title" autofocus placeholder="一句话记下它" /></label><label>详细内容<textarea ref="contentInput" v-model="seedForm.content" rows="9" v-on:input="resizeContentFromEvent" placeholder="背景、想法或验收方式……"></textarea></label><div v-if="editingSeed" class="seed-timestamps"><span>创建时间<strong>{{ formatTime(editingSeed.createdAt) }}</strong></span><span>开始时间<strong>{{ formatTime(editingSeed.startedAt) }}</strong></span><span>完成时间<strong>{{ formatTime(editingSeed.completedAt) }}</strong></span><span>耗时<strong>{{ formatDuration(editingSeed.durationSeconds) }}</strong></span></div><div class="actions"><button type="button" class="quiet" @click="seedDialog = false">取消</button><button class="primary">{{ editingId ? '保存修改' : '播下种子' }}</button></div></form>
  </div>
  <div v-if="error" class="toast">{{ error }}</div>
</template>
