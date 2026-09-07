---
name: macos-hig-wails
description: Apple Human Interface Guidelines mapped to Wails v3 (Go) + Svelte 5 + Tailwind v4 desktop windows. Semantic system colors, SF type scale, toolbar/sidebar/split-view anatomy, buttons, alerts, progress, context menus, system materials. Use when building or reviewing any macOS window in this stack.
---

<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.
-->

# macOS HIG for Wails Skill

> Apple Human Interface Guidelines, translated to the Wails v3 + Svelte 5 + Tailwind v4 stack. Every rule below is either verbatim Apple guidance (with its HIG page) or a mechanical mapping of Apple guidance onto this stack. When the two conflict, the HIG page wins and the mapping says so honestly.

---

## 0. HOW TO USE THIS SKILL

1. Read the brief, then pull only the sections that fit. None of this fires automatically.
2. Apple rules are quoted as rules. Framework mappings are marked `Map:`. Do not present a mapping as Apple guidance.
3. Webviews cannot reach APIs the HIG assumes (NSColor, SF Symbols font, vibrancy). Each such gap has exactly one honest fallback, documented below. Never invent a second fallback per gap.
4. Output a one-line "HIG Read" before generating: **"Reading this as: \<window kind> for \<task>, following HIG \<pages>, with \<deviations or none>."**

---

## 1. WINDOWS (HIG: Windows, Designing for macOS)

- A primary window presents main navigation, content, and actions. An auxiliary window presents one task, allows no navigation, and closes when done. Know which one is being built.
- Never build custom window frames or controls, and never imitate the system appearance by hand. A near-miss reads as broken.
- Windows must adapt fluidly to resizing. Set explicit minimum and maximum sizes so content never overlaps and dividers never vanish.
- Never put critical information or actions in a bottom bar. Window bottoms get hidden when people relocate windows. A bottom bar carries at most a small status line about the window's contents.
- Support full-screen mode unless there is a stated reason not to.
- Inactive windows drop vibrancy and mute their controls. Custom chrome must reproduce the state change; system chrome does it free.
- `Map:` use the system window backdrop. The pre-content launch color is a flat neutral the CSS window token paints over on load, never a branded splash. Reserve leading-edge clearance in any custom toolbar for the traffic lights when the title bar is hidden-inset.

---

## 2. TOOLBARS (HIG: Toolbars, macOS section)

- Anatomy is fixed: leading edge (navigation, sidebar toggle, view title), center area (common controls, collapsible into overflow), trailing edge (primary action, inspector toggles, search, More menu). Maximum three groups.
- Never title a window with the app name. Titles name the content, stay under 15 characters, and leave room for controls.
- One prominent action per view, trailing side, tinted with the accent (`.prominent` style). Prominence is the only way to mark the preferred choice; never resize buttons to signal priority.
- Prefer system symbols without borders in toolbar items. Borders are unnecessary; hover and selection states come from the system.
- Use text instead of a symbol when a short label communicates more clearly (verbs like Edit, actions with no standard glyph).
- Reduce custom toolbar backgrounds and tints. Custom paint fights system background effects; let the content layer inform the toolbar.
- `Map:` in a webview toolbar, traffic-light clearance is manual leading padding. Toolbar height matches the title-bar inset. No icon library is required when every action is text-appropriate; adding one later means picking exactly one family for the whole project, never mixing.

---

## 3. SIDEBARS AND SPLIT VIEWS (HIG: Sidebars, Split Views, macOS sections)

- Show at most two hierarchy levels in a sidebar. Deeper hierarchies get a content list between sidebar and detail.
- Group labels are succinct and descriptive, with unnecessary words omitted.
- Persistently highlight the current selection in every pane that leads to the detail view.
- Sidebar icons default to the app accent color and must follow the user's system accent choice. Fixed colors are allowed only sparingly, to encode meaning (the VIP-icon pattern), never as decoration.
- Offer a way to hide the sidebar, but never hide it by default.
- Never park critical information or actions at the sidebar bottom.
- Split-view dividers: prefer the thin (1pt) style. Set reasonable minimum and maximum pane sizes. Dividers that disappear read as layout bugs.
- `Map:` selection fill is the accent token (which itself follows the system accent). Sidebar/content separation is a fill step plus the 1pt divider.

---

## 4. COLOR (HIG: Color, macOS section; App Accent Colors)

- Never hard-code system color values; they shift between releases and contexts. Reference them semantically.
- Never redefine a semantic color's meaning. Separator color is not text color; secondary-label color is not a background.
- Never use one color to mean two different things, especially status vs interactivity.
- Every custom color ships light, dark, and increased-contrast variants. Even single-appearance apps define both.
- Minimum contrast 4.5:1 for text; target 7:1 for small custom-colored text.
- Never rely on color alone to communicate. Pair it with labels or glyph shapes.
- Accent color (macOS 11+): the system applies the user's chosen accent over the app's accent unless the setting is multicolor. Fixed-color icons keep their color; everything else follows the user.
- `Map:` the macOS NSColor table becomes CSS custom properties, documented as web approximations: `windowBackgroundColor` to `--window-bg`, `controlBackgroundColor` to `--control-bg`, `labelColor` / `secondaryLabelColor` / `tertiaryLabelColor` to `--label` / `--secondary-label` / `--tertiary-label`, `separatorColor` / `gridColor` to `--separator` / `--grid`, `selectedContentBackgroundColor` to `--selection`, `keyboardFocusIndicatorColor` to `--focus-ring`, `controlAccentColor` to `--accent` via the `AccentColor` keyword with a system-blue fallback. Tailwind v4 `@theme inline` exposes them as utilities. No other hues exist as tokens; state dots use system green/amber/red fixed values.

---

## 5. TYPOGRAPHY (HIG: Typography, macOS section)

- SF Pro is the macOS system font. New York is available for Catalyst apps. Prefer system fonts; custom fonts must implement Dynamic Type equivalents and accessibility behaviors (macOS itself does not do Dynamic Type, but text must still scale sanely).
- The macOS text-style table, verbatim: Large Title Regular 26, Title 1 Regular 22, Title 2 Regular 17, Title 3 Regular 15, Headline Bold 13, Body Regular 13, Callout Regular 12, Subheadline Regular 11, Footnote Regular 10, Caption 1 Regular 10, Caption 2 Medium 10. Minimum size 10pt. Avoid Ultralight, Thin, and Light weights.
- Convey hierarchy with weight, size, and color. Minimize typeface count; mixing faces obscures hierarchy.
- Match standard-control text with the dynamic system font variants (label, control-content, menu, title-bar, user fixed-pitch for monospaced data).
- `Map:` `-apple-system` stack with `font-feature-settings` and tabular numerals for machine data; SF Mono (via `ui-monospace`) for addresses, hashes, and code only, never for prose or decoration. Web px maps 1:1 to pt at standard density.

---

## 6. MATERIALS (HIG: Materials, macOS section; Liquid Glass)

Apple's rules, faithfully:

- Liquid Glass is a functional-layer material for controls and navigation (tab bars, sidebars), floating above the content layer. It is never content-layer material.
- Apply Liquid Glass effects sparingly and only to the most important functional elements. Overuse distracts from content.
- Use the regular variant when background content threatens legibility or when the element carries significant text (alerts, sidebars, popovers). Use clear only over rich media backgrounds, with a dimming layer on bright content.
- Apply color to Liquid Glass sparingly: reserved for primary actions (background, not label) and genuine status emphasis. Never tint multiple controls. Symbols and text on glass default to monochrome.
- Standard materials and vibrancy convey structure beneath glass. Choose them by semantic meaning, never by the apparent color they impart. Use vibrant colors on top of materials for legibility.
- macOS specifics: allow vibrancy in custom views deliberately and test across contexts; choose a background blending mode (behind-window vs within-window) that complements the design.

---

## 7. DARK MODE (HIG: Dark Mode)

- Never offer an app-specific appearance setting. It doubles the user's work and reads as broken when the app ignores the system choice.
- The app must look good in light, dark, and Auto (which can switch mid-session).
- Use semantic colors; they adapt. Custom colors get explicit bright and dim variants.
- Soften pure-white imagery so it does not glow in dark contexts.
- Labels use the system label-color ramp; text fields use system views so vibrancy applies.
- macOS desktop tinting: keep custom component backgrounds neutral so they harmonize when the window picks up desktop color.
- `Map:` `prefers-color-scheme` switches the token set. No theme toggle, no persisted theme, no `[data-theme]` overrides.

---

## 8. BUTTONS (HIG: Buttons, macOS section)

- Minimum hit region 44x44pt. Space buttons so each is visually distinct and activatable.
- Always include a press state on custom buttons. A button without one feels dead.
- One or two prominent buttons per view, maximum. Prominence draws the eye; abundance of prominence is cognitive load.
- Button content is symbol, text label, or both. Associate familiar actions with familiar icons; prefer standard symbols. Write text labels as short verb-led phrases in title-style capitalization.
- Roles: Normal, Primary (default, answers Return, uses the accent), Cancel, Destructive (system red). Never assign Primary to a destructive action.
- macOS push buttons: default button where appropriate; append a trailing ellipsis when the button opens another window, view, or app; consider spring loading for drag targets.
- Square buttons live in views near their target (tables: add/remove rows), never in toolbars or status bars, symbols preferred, no introductory labels.
- Help buttons: system-provided, at most one per window, placed in-view (dialog corner opposite dismissal buttons, or settings corner), never in the frame, never introduced with text.
- Image buttons need ~10px padding between image and clickable edge, live in views not frames, labels go below.
- Activity feedback: a button may show an activity indicator with a label swap ("Checkout" to "Checking out…") while the action runs.
- `Map:` prominent = accent fill + accent-text, 6px radius, disabled at 50% opacity, `active:translate-y-[1px]` press state, inline 12px spinner + label swap for busy. Standard push = bordered control fill. Destructive confirm = destructive fill with a plain Keep/Cancel escape.

---

## 9. ALERTS (HIG: Alerts, macOS section)

- Use alerts sparingly; each one interrupts. Every alert must carry essential information plus useful actions.
- Never alert merely to inform. Informational states belong inline in context (the Mail pattern: an indicator the user can expand).
- Never alert for common undoable actions. Never alert at launch; show cached content plus a nonintrusive label instead.
- Titles name the situation completely and specifically in one to two lines, never "Error" or a code. Sentence case for sentences, title case for fragments.
- Informative text only when it adds value, kept to short complete sentences.
- Never explain the buttons in the body copy. Button titles are one-to-two-word verbs describing the result ("Erase", "Convert", "Ignore"); informational alerts may use OK; anything cancellable offers Cancel. Cancel is never the default.
- Button order: default (most likely, nondestructive) trailing or top; Cancel leading or bottom. Destructive styling only for unintended destructive consequences; deliberately chosen destructive actions keep their plain title so Return confirms intent.
- macOS extras: system shows the app icon (custom symbol optional); repeating alerts may offer suppression; accessory views and Help buttons allowed; caution symbol only when genuinely extra attention is required.
- `Map:` version gates and errors render as inline rows in context with the canonical message verbatim, following the informative-text rules for copy.

---

## 10. PROGRESS INDICATORS (HIG: Progress Indicators, macOS section)

- Two kinds: determinate (known duration: bars, circular tracks) and indeterminate (unknown: spinners, and on macOS also indeterminate bars).
- Prefer determinate whenever the duration is knowable. Report advancement honestly; a bar that parks at 90% reads as frozen or deceptive.
- Keep indicators moving. A stationary indicator means "stalled" to the user; stalls get explanatory feedback, not silence.
- Never switch shapes mid-task (spinner to bar). Consistency of location matters too: status always lives in the same place.
- Context descriptions must be accurate and specific, never "Loading…" or "Authenticating…".
- Offer Cancel where halting is safe (plus Pause where partial progress matters); warn when halting destroys progress.
- macOS: prefer the small spinner for background operations and constrained spaces (next to the control it belongs to). Never label a spinner; its presence beside the initiating action is the label.
- `Map:` CSS spinner is border-rotation on `transform` only, 12px beside the initiating button or status line, killed under `prefers-reduced-motion`. No perpetual ambient loops anywhere.

---

## 11. CONTEXT MENUS (HIG: Context Menus, macOS notes)

- Menus are small and strictly relevant to the target: the commands the user most likely needs right there, nothing advanced or rare.
- Hide unavailable items; never dim them. (Cut/Copy/Paste are the exempted regulars.)
- Every context item must also exist in the main interface. Context menus shortcut; they never warehouse.
- Be consistent: if some same-kind items have menus, all of them do, or users read the gap as a bug.
- At most one submenu level, with a predictive title. At most ~three separator groups. No keyboard shortcuts shown (the menu itself is the shortcut).
- Destructive items go last and read as destructive (red).
- Labels are short action descriptions; a title appears only when it clarifies scope (e.g. a selection count).
- Use the same icons as the system for Copy, Share, Delete wherever icons appear.
- `Map:` the webview's native menu is suppressed in production builds only; dev builds keep it behind Option-right-click so the inspector stays reachable. One menu component serves all targets, clamps to the viewport, and dismisses on Escape, outside click, scroll, or resize. Destructive actions confirm inline in two steps, never in a modal.

---

## 12. SF SYMBOLS HONESTY NOTE (HIG: SF Symbols)

- Symbols convey objects and concepts wherever icons appear: toolbars, lists, menus, inline text.
- Match symbol weight to adjacent text weight; pick scale (small/medium/large) for emphasis without breaking the match. Outline variant for toolbars and lists; fill where selection or emphasis needs it. Monochrome default; hierarchical/palette/multicolor only with intent. Animate only to communicate (feedback, status, progress), never as ambience.
- Never use symbols (or confusing lookalikes) in app icons, logos, or trademarks.
- `Map:` webviews cannot load the SF Symbols font, so symbols are unavailable in this stack. The compliant fallback is text labels wherever HIG permits them (short verb-led phrases). Never hand-draw glyph paths as a substitute. If a future surface genuinely needs glyphs, adopt exactly one maintained icon family for the whole project and document the deviation here.

---

## 13. PRE-FLIGHT CHECK

Run every box before shipping a window. One failure means not done.

- [ ] HIG Read declared (Section 0)?
- [ ] Window kind correct (primary vs auxiliary), system chrome untouched, min/max sizes set?
- [ ] Toolbar anatomy leading/center/trailing, title is content under 15 chars (never the app name), exactly one prominent action trailing?
- [ ] Sidebar at most two hierarchy levels, succinct labels, selection persistent, icons in system accent, not hidden by default?
- [ ] Thin 1pt dividers, panes resizable within sane limits?
- [ ] Colors all semantic (label ramp, separator, selection, accent), light + dark + contrast variants, no double-meaning color, contrast 4.5:1 minimum?
- [ ] Type on the macOS table (13 body max for UI, 10 floor, no light weights), mono only for machine data?
- [ ] Materials per Section 6 (functional layer only, correct variant, color sparingly)?
- [ ] Appearance follows the system only, no theme toggle, both modes visually checked?
- [ ] Buttons: prominence count, title-style verb labels, ellipsis where it opens elsewhere, press states, busy pattern, destructive never primary?
- [ ] Alerts only for decisions, inline rows for information, specific titles, verb buttons, Cancel never default?
- [ ] Progress honest and consistently placed, spinners unlabeled, nothing ambient looping, reduced-motion safe?
- [ ] Context menus small/relevant/consistent, unavailable hidden, destructive last and red, all items mirrored in main UI, native menu suppressed in production?
- [ ] Zero hand-drawn glyphs; text labels where symbols are unavailable?
- [ ] Zero em-dashes in visible copy?
- [ ] Motion is `transform`/`opacity` only, each animation justifiable in one sentence?
