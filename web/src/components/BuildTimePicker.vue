<script setup lang="ts">
/**
 * Build-time field: a thin adapter from the domain's local wall-clock string
 * (`YYYY-MM-DDTHH:mm:ss`, see domain.parseLocalTime / toLocalInput) to Naive's
 * NDatePicker.
 *
 * Naive owns every visible control — the trigger input, the calendar panel, the
 * hour/minute/second columns and the clear/now/confirm actions. This component
 * only bridges the value and keeps the two guarantees the domain needs:
 *
 *  - `isTimeDisabled` refuses wall times that do not exist on the selected day
 *    (a daylight-saving gap) or that are not strictly after the Unix epoch.
 *  - Text Naive did not accept as the committed value is remembered, so a stale
 *    timestamp can never be published silently. Naive teleports the datetime
 *    panel into the modal, outside this component, so the panel's own date and
 *    time inputs are watched at the document level while it is open: a date it
 *    cannot parse, and a time a daylight-saving transition moves (Naive parses a
 *    typed time against today and then re-anchors it, so 02:30 on 2026-03-08 in
 *    New York silently becomes 03:30) keep the field unsubmittable until a value
 *    Naive accepts is in place.
 *
 * The panel is also controlled through `show`, so Escape cancels this popup
 * instead of reaching the modal that hosts it.
 */
import { computed, nextTick, onBeforeUnmount, ref } from 'vue'
import { NDatePicker, NText, type DatePickerInst } from 'naive-ui'
import { parseLocalTime, toLocalInput } from '../domain'

const props = defineProps<{
  /** `YYYY-MM-DDTHH:mm:ss` local wall time, or '' when unset. */
  modelValue: string
  disabled: boolean
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: string): void
}>()

/** Naive's time validator shape (time-picker's IsHour/Minute/SecondDisabled). */
interface TimeValidator {
  isHourDisabled?: (hour: number) => boolean
  isMinuteDisabled?: (minute: number, hour: number | null) => boolean
  isSecondDisabled?: (second: number, minute: number | null, hour: number | null) => boolean
}

const actions: Array<'clear' | 'now' | 'confirm'> = ['clear', 'now', 'confirm']
const yearRange: [number, number] = [1, 9999]

/** Naive's datetime panel, teleported out of this component into the modal. */
const PANEL_SELECTOR = '.n-date-panel--datetime'
const PANEL_DATE_INPUT = '.n-date-panel-date-input input'
const PANEL_TIME_INPUT = '.n-time-picker input'

const pickerRef = ref<DatePickerInst | null>(null)
/** Controlled panel visibility, so Escape can cancel this popup only. */
const panelShow = ref(false)
/** Text Naive's trigger input holds that it has not accepted. */
const triggerRaw = ref('')
/** Text typed in the open panel's inputs that Naive has not accepted. */
const panelDateRaw = ref('')
const panelTimeRaw = ref('')
/** Panel text dropped by closing the panel without an accepted value. */
const droppedRaw = ref('')
/** Only surfaced after a submit attempt, so typing stays quiet. */
const showError = ref(false)
/** Set by a rejected panel confirm and consumed by the following update:value. */
const confirmRejected = ref(false)
/** Pointer press outside the panel distinguishes submission from cancellation. */
let outsidePress: Element | null = null
/** A refused confirm must keep its complaint even though the panel closes. */
let keepRaws = false

const timestamp = computed(() => {
  const date = parseLocalTime(props.modelValue)
  return date ? date.getTime() : null
})

/** A wall time the domain accepts: it exists locally and follows the epoch. */
function isSelectableLocalTime(value: string): boolean {
  const date = parseLocalTime(value)
  return date !== null && date.getTime() > 0
}

const panelTypedText = computed(() => {
  const date = panelDateRaw.value
  const time = panelTimeRaw.value
  if (date !== '' && time !== '') return `${date} ${time}`
  return date || time
})

const errorMessage = computed(() => {
  const typed = triggerRaw.value || panelTypedText.value || droppedRaw.value
  if (typed !== '') {
    return `“${typed}”不是有效的本地时间，请重新输入或从面板中选择。`
  }
  if (props.modelValue !== '' && !isSelectableLocalTime(props.modelValue)) {
    return '构建时间无效：必须是晚于 Unix 纪元的有效本地时间。'
  }
  return ''
})

/**
 * The instant a wall time maps to, or null when the engine has to normalise it
 * (a daylight-saving gap) — the same round trip parseLocalTime performs.
 */
function localInstant(
  year: number,
  month0: number,
  day: number,
  hour: number,
  minute: number,
  second: number,
): number | null {
  const date = new Date(0)
  date.setFullYear(year, month0, day)
  date.setHours(hour, minute, second, 0)
  const exact =
    date.getFullYear() === year &&
    date.getMonth() === month0 &&
    date.getDate() === day &&
    date.getHours() === hour &&
    date.getMinutes() === minute &&
    date.getSeconds() === second
  return exact ? date.getTime() : null
}

function isTimeDisabled(value: number): TimeValidator {
  const date = new Date(value)
  const year = date.getFullYear()
  const month0 = date.getMonth()
  const day = date.getDate()
  const allowed = (hour: number, minute: number, second: number): boolean => {
    const instant = localInstant(year, month0, day, hour, minute, second)
    return instant !== null && instant > 0
  }
  return {
    // A unit stays offered while it holds at least one possible time, so a
    // half-hour daylight-saving gap keeps the minutes that do exist selectable;
    // seconds are judged exactly. An unset hour/minute never disables the
    // fields below it.
    isHourDisabled: (hour) => !allowed(hour, 0, 0) && !allowed(hour, 59, 59),
    isMinuteDisabled: (minute, hour) =>
      hour !== null && !allowed(hour, minute, 0) && !allowed(hour, minute, 59),
    isSecondDisabled: (second, minute, hour) =>
      hour !== null && minute !== null && !allowed(hour, minute, second),
  }
}

/** Strict instant the panel's two texts describe, or null when they do not. */
function panelWallTime(dateText: string, timeText: string): number | null {
  const date = parseLocalTime(`${dateText}T${timeText}`)
  return date ? date.getTime() : null
}

/**
 * Naive's own panel input accepted a keystroke but stored a different wall time
 * (a daylight-saving transition moved it): remember the text the user typed.
 */
function recordPanelTyping(panel: HTMLElement, target: HTMLInputElement, typed: string): void {
  const dateInput = panel.querySelector<HTMLInputElement>(PANEL_DATE_INPUT)
  const timeInput = panel.querySelector<HTMLInputElement>(PANEL_TIME_INPUT)
  if (target !== dateInput && target !== timeInput) return
  const dateText = dateInput?.value ?? ''
  const timeText = timeInput?.value ?? ''
  if (dateText === '' || timeText === '') {
    // Naive restores an emptied input and keeps its own value: nothing to keep.
    panelDateRaw.value = ''
    panelTimeRaw.value = ''
    return
  }
  if ((target === dateInput ? dateText : timeText) !== typed) {
    panelDateRaw.value = target === dateInput ? typed : ''
    panelTimeRaw.value = target === timeInput ? typed : ''
    return
  }
  const instant = panelWallTime(dateText, timeText)
  if (instant !== null && instant > 0) {
    panelDateRaw.value = ''
    panelTimeRaw.value = ''
    return
  }
  panelDateRaw.value = dateText
  panelTimeRaw.value = timeText
}

function handlePanelInput(event: Event): void {
  const target = event.target
  if (!(target instanceof HTMLInputElement)) return
  const panel = target.closest<HTMLElement>(PANEL_SELECTOR)
  if (!panel) return
  const typed = target.value
  // Naive's handler runs after this capture listener and re-derives the input,
  // so the comparison has to wait for that render.
  void nextTick(() => {
    if (panel.isConnected) recordPanelTyping(panel, target, typed)
  })
}

/** A selection replaces only its own field; pure confirmation replaces neither. */
function handlePanelClick(event: Event): void {
  const target = event.target
  if (!(target instanceof Element) || !target.closest(PANEL_SELECTOR)) return
  const day = target.closest('[data-n-date]')
  if (day && !day.classList.contains('n-date-panel-date--disabled')) {
    panelDateRaw.value = ''
    return
  }
  const timeItem = target.closest('.n-time-picker-col__item')
  const actionButton = target.closest('button')
  const timeChanged =
    (timeItem !== null && !timeItem.classList.contains('n-time-picker-col__item--disabled')) ||
    (actionButton !== null && actionButton.closest('.n-time-picker-actions') !== null &&
      !actionButton.classList.contains('n-time-picker-actions__confirm'))
  if (timeChanged) {
    panelTimeRaw.value = ''
    return
  }
  if (actionButton !== null && actionButton.closest('.n-date-panel-actions') !== null &&
    !actionButton.classList.contains('n-button--primary-type')) {
    panelDateRaw.value = ''
    panelTimeRaw.value = ''
  }
}

function handleOutsidePress(event: Event): void {
  const target = event.target
  if (!(target instanceof Element) || target.closest(PANEL_SELECTOR)) return
  outsidePress = target
}

/** A press on a control that submits the publish form. */
function isSubmitPress(target: Element | null): boolean {
  if (!target) return false
  return target.closest('button[type="submit"], input[type="submit"]') !== null
}

function handleDocumentKeydown(event: Event): void {
  if (!(event instanceof KeyboardEvent) || event.key !== 'Escape' || !panelShow.value) return
  if (event.isComposing) return
  const target = event.target
  if (!(target instanceof Element)) return
  const panel = target.closest<HTMLElement>(PANEL_SELECTOR)
  if (!panel) return
  // The modal's focus trap listens for Escape on the document, so without this
  // the whole publish dialog would close. Cancel the innermost popup instead:
  // while the inline time menu is open Naive closes it itself and marks the
  // event, so only the remaining cases are taken over here.
  const timePicker = target.closest('.n-time-picker')
  if (timePicker !== null && timePicker.querySelector('.n-time-picker-panel') !== null) return
  event.stopPropagation()
  event.preventDefault()
  closePanel()
}

/** Cancel the panel the way Naive's own close does: roll the pending value back. */
function closePanel(): void {
  panelShow.value = false
  handlePanelShow(false)
  pickerRef.value?.focus()
}

function togglePanelListeners(attach: boolean): void {
  const listen = (type: string, handler: EventListener): void => {
    if (attach) document.addEventListener(type, handler, true)
    else document.removeEventListener(type, handler, true)
  }
  listen('input', handlePanelInput)
  listen('click', handlePanelClick)
  listen('pointerdown', handleOutsidePress)
  listen('keydown', handleDocumentKeydown)
}

function handlePanelShow(show: boolean): void {
  panelShow.value = show
  togglePanelListeners(show)
  if (show) return
  // Closing drops the panel's pending value. Keep the complaint only when the
  // press was the submit itself (the draft would otherwise publish the value the
  // user replaced) or a confirm was refused; a cancel drops it, because Naive
  // rolled the pending value back to the committed one.
  const typed = panelTypedText.value
  if (typed !== '') {
    droppedRaw.value = keepRaws || isSubmitPress(outsidePress) ? typed : ''
  }
  keepRaws = false
  outsidePress = null
  panelDateRaw.value = ''
  panelTimeRaw.value = ''
}

/**
 * Refuse a panel confirm whose committed value does not match the text the user
 * typed: Naive normalises a wall time a daylight-saving transition skips instead
 * of rejecting it, and the normalised timestamp must not be trusted.
 */
function handleConfirm(value: number | null): void {
  if (value === null || (panelDateRaw.value === '' && panelTimeRaw.value === '')) {
    confirmRejected.value = false
    return
  }
  const [dateText, timeText] = toLocalInput(value / 1000).split('T')
  const instant = panelWallTime(panelDateRaw.value || dateText, panelTimeRaw.value || timeText)
  confirmRejected.value = instant === null || instant !== value || value <= 0
  if (confirmRejected.value) keepRaws = true
}

function handleUpdate(value: number | null): void {
  if (confirmRejected.value) {
    // The confirm is dropped: the draft keeps the value Naive had already
    // accepted, and the remembered text keeps blocking the submit.
    confirmRejected.value = false
    showError.value = true
    return
  }
  // Naive accepted a value (typed text, panel pick, now or clear): the inputs
  // and the draft agree again.
  triggerRaw.value = ''
  panelDateRaw.value = ''
  panelTimeRaw.value = ''
  droppedRaw.value = ''
  emit('update:modelValue', value === null ? '' : toLocalInput(value / 1000))
}

/**
 * Capture-phase listener for Naive's trigger input: remember the text a
 * keystroke leaves behind. Accepted text is followed by `update:value`, which
 * clears it, so anything still remembered at submit time was rejected by Naive
 * and must not be replaced by the previously committed timestamp.
 */
function handleInput(event: Event): void {
  const target = event.target
  if (target instanceof HTMLInputElement) triggerRaw.value = target.value
}

/** Called by the publish form before it submits; focuses the input on failure. */
function validate(): boolean {
  if (errorMessage.value !== '') {
    showError.value = true
    pickerRef.value?.focus()
    return false
  }
  showError.value = false
  return true
}

onBeforeUnmount(() => {
  togglePanelListeners(false)
})

defineExpose({ validate })
</script>

<template>
  <div
    class="build-time-picker"
    @input.capture="handleInput"
    @keydown.enter.prevent.stop
  >
    <NDatePicker
      ref="pickerRef"
      type="datetime"
      :show="panelShow"
      :value="timestamp"
      :disabled="disabled"
      :clearable="true"
      :actions="actions"
      :year-range="yearRange"
      format="yyyy-MM-dd HH:mm:ss"
      time-picker-format="HH:mm:ss"
      :status="showError && errorMessage !== '' ? 'error' : undefined"
      :is-time-disabled="isTimeDisabled"
      @update:show="handlePanelShow"
      @confirm="handleConfirm"
      @update:value="handleUpdate"
    />
    <NText v-if="showError && errorMessage !== ''" type="error" role="alert">
      {{ errorMessage }}
    </NText>
  </div>
</template>
