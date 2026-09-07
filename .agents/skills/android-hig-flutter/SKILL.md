---
name: android-hig-flutter
description: Apple Human Interface Guidelines (iOS) mapped to Flutter + Material 3 on Android. Designing for iOS, layout, tab bars, toolbars, color, typography, materials, dark mode, buttons, alerts/sheets, progress, menus, icons/SF Symbols. Use when building or reviewing any Android Flutter screen.
---

<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.
-->

# Android HIG for Flutter Skill

> Apple Human Interface Guidelines, translated to the Flutter + Material 3 (Android) stack. Every rule below is either verbatim Apple guidance (with its HIG page) or a mechanical mapping of Apple guidance onto this stack. When the two conflict, the HIG page wins and the mapping says so honestly.

---

## 0. HOW TO USE THIS SKILL

1. Read the brief, then pull only the sections that fit. None of this fires automatically.
2. Apple rules are quoted as rules. Flutter/Material mappings are marked `Map:`. Do not present a mapping as Apple guidance.
3. Flutter cannot reach every platform API the HIG assumes (Dynamic Type on Android, SF Symbols font, vibrancy). Each such gap has exactly one honest fallback, documented below. Never invent a second fallback per gap.
4. Output a one-line "HIG Read" before generating: **"Reading this as: <screen kind> for <task>, following HIG <pages>, with <deviations or none>."**

---

## 1. DESIGNING FOR iOS (HIG: Designing for iOS)

- **Display.** iPhone: medium-size, high-resolution display.
- **Ergonomics.** People hold the device in one or both hands, switch portrait/landscape, viewing distance 1-2 feet. Controls in the **middle or bottom area** are easier to reach (HIG: Best practices — accommodate the way people hold their device).
- **Inputs.** Multi-Touch gestures, virtual keyboards, voice. Tasks happen on the go, in 1-minute bursts or hour-long sessions, switching apps frequently.
- **Best practices, verbatim:** help people concentrate on primary tasks by **limiting the number of onscreen controls** while making secondary details discoverable; **adapt seamlessly to appearance changes** — orientation, Dark Mode, Dynamic Type; **support bottom/middle reachable area** (e.g. swipe to navigate back or initiate list-row actions); integrate platform capabilities with permission (payments, biometrics, location).

- `Map:` `Scaffold` + `SafeArea` with `WindowInsets`, no custom window chrome. `MediaQuery` for rotation; `NavigationBar` at bottom aligns with reachable area. Respect `textScaler`/`disableAnimations`. No macOS `main/key/inactive` window states.

---

## 2. LAYOUT AND ADAPTABILITY (HIG: Layout — iOS section)

- Group related items (negative space, background shapes, colors, materials, separators) and keep controls distinct from content.
- Make essential information easy to find — give it space, don't crowd with secondary details.
- Extend content to fill screen/window; backgrounds and scrollable layouts go edge-to-edge. Controls/navigation (`TabBar`/`NavigationBar`) float **on top** of content, not on same plane.
- Differentiate controls from content using Liquid Glass material; use scroll-edge effect at transitions, not a custom background.
- Place important items near **top and leading side** (reading order); align components to communicate hierarchy.
- Adaptability: design layout that **adapts gracefully to context changes while remaining recognizably consistent** — different screen sizes, orientations, Dynamic Island/camera cutouts, Display Zoom, Dynamic Type. Preview on largest and smallest layouts; respect safe areas/margins.
- iOS: **support both portrait and landscape**; avoid full-width buttons (inset from edges, align with safe areas + curvature); hide status bar only when it adds value.
- `Map:` `LayoutBuilder` + `SafeArea` + `CustomScrollView`/`SliverAppBar` with `scrolledUnderElevation`. Breakpoints: compact (<600dp) / medium / expanded via `NavigationSuite`. Replace `backgroundExtensionEffect` with `extendBodyBehindAppBar` + tonal `surfaceContainer`. Never paint a toolbar/sidebar background with a hard hex.

---

## 3. NAVIGATION — TAB BARS FIRST (HIG: Tab Bars — iOS; Sidebars, Split Views)

- **Use a tab bar to support navigation, not actions.** Navigates among sections (Alarm/Stopwatch/Timer). For actions on current view, use a toolbar instead (HIG: Tab Bars Best practices).
- Keep tab bar **visible when navigating**; hiding makes people forget where they are (except modal covers it).
- Use appropriate number of tabs; **avoid overflow / More tab** — trailing More hides content and hurts reach.
- **Don't disable or hide tab bar buttons when content unavailable.** Explain empty state instead.
- **Include labels** beneath/beside icons; single-word labels. Prefer `SF Symbols` filled variants, auto-adapting to compact (icon above label) vs regular (icon beside label).
- **Badge** only for critical new info (red oval, number or `!`).
- Avoid similar color on tab labels and content background; use monochromatic tab bar or high-differentiation accent (Liquid Glass color).
- iOS detail: tab bar floats above content on **Liquid Glass** background; can minimize with accessory (MiniPlayer) — `TabBarMinimizeBehavior`.
- **Sidebars are not the iPhone default.** On iOS, sidebar needs large vertical/horizontal space; tab bar (or `sidebarAdaptable` on iPad) is the alternative for complex hierarchies. Do not start with a hidden sidebar on a phone. Sidebar usable only on **expanded widths** (tablet/foldable) as `NavigationDrawer`/`NavigationRail`.
- Split views: iOS — **prefer in regular, not compact** environment; needs horizontal space. Persistently highlight current selection in each pane leading to detail; consider drag-and-drop between panes; thin (1pt) divider, keep divider visible with min/max pane sizes.
- `Map:` phone → `NavigationBar` 3-5 items; tablet/fold → `NavigationRail` (600-840dp) or `NavigationDrawer` (≥840dp). One active destination highlighted via `ColorScheme.secondaryContainer`/primary indicator. Split-view → use `Row` + `VerticalDivider(width:1)` + `NavigationSplitView`-like two-pane with `LayoutBuilder`.

---

## 4. TOOLBARS (HIG: Toolbars — iOS section)

- Choose items deliberately to avoid overcrowding; define which items move to **overflow / More menu**.
- Add More menu for less important actions; prioritize less important there.
- **Reduce use of toolbar backgrounds and tinted controls** — let content layer inform color; use `ScrollEdgeEffectStyle` to distinguish toolbar from content.
- Prefer system symbols **without borders**; borders unnecessary, system provides hover/selection.
- Prefer recognizable symbols; for actions poorly represented by symbols (e.g. Edit) use **short verb-led text**.
- **Use `.prominent` style for one key action** (Done/Submit) — separates and tints; **only one primary**, on trailing side. Prominence via accent color, not size.
- Titles: provide useful title per view (not app name), **under 15 chars** to leave room for controls; Back/Close use **standard symbols only**, no `Back`/`Close` text label.
- Item groupings: leading (Back/Close, sidebar toggle, title), center (common controls, collapses to overflow), trailing (primary action, inspectors, search, More). Max ~three groups.
- `Map:` `AppBar` with `centerTitle:false`, `leading` auto `BackButton`, `actions` 1-3 icons then `PopupMenuButton`. Prominent → `FilledButton` (primary) at trailing `actions` or content FAB — never resize `IconButton` to signal priority. `scrolledUnderElevation` only, no custom `surfaceTintColor` or barrel backgrounds.

---

## 5. COLOR (HIG: Color — Best practices, Inclusive color, System colors, Liquid Glass color; iOS tables)

- Don't use same color to mean different things (especially status vs interactivity) — be consistent.
- Make colors work in **light, dark, and increased-contrast** contexts; provide light/dark variants for every custom color, plus increased-contrast variant, even if app ships single-appearance (supports Liquid Glass adaptivity).
- Inclusive: **never rely solely on color** to differentiate/communicate. Pair with labels/glyph shapes; ensure contrast for color-blind.
- Avoid hard-coding system color values; they fluctuate. Use semantic APIs (`Color`/`UIColor`).
- Avoid redefining semantic meanings (`separator` is not text color).
- **Liquid Glass color:** by default Liquid Glass has no inherent color (takes behind-layer color). Apply color sparingly — **only** for genuine emphasis (status, primary action background — not label). Never tint backgrounds of multiple controls; symbols/text on glass default to **monochrome**.
- Avoid similar colors in control labels vs colorful content background — prefer monochromatic tab bar/toolbar.
- iOS system colors define **systemGrouped vs system** backgrounds (primary/secondary/tertiary for hierarchy) and foreground ramp (`label`, `secondaryLabel`, `tertiaryLabel`, `quaternaryLabel`, `separator`, `link`).
- `Map:` Material `ColorScheme` is the semantic token set. This repo: `ColorScheme.fromSeed(seedColor: Colors.teal)` `useMaterial3:true` → `seed` light + `brightness:Brightness.dark` dark. Reference only roles (`primary`, `onPrimary`, `surface`, `onSurface`, `surfaceContainer`, `outline`, `error`, `secondaryContainer`) — never `Color(0xFF...)` inline. Status dots use system `green`/`amber`/`red` fixed roles + label/icon.

---

## 6. TYPOGRAPHY (HIG: Typography — iOS, Supporting Dynamic Type)

- Use legible sizes: **iOS default 17pt, minimum 11pt** (spec table). Avoid Ultralight/Thin/Light — prefer Regular/Medium/Semibold/Bold.
- Convey hierarchy with weight/size/color; minimize typeface count.
- System fonts: **SF Pro** (sans; compact/arabic/hebrew variants) + **NY** (serif); SF Symbols weights match SF. Use **text styles** (`LargeTitle, Title1/2/3, Headline, Body, Callout, Subhead, Footnote, Caption1/2`) — each defines weight/size/leading per Dynamic Type size, scaling proportionately.
- Custom fonts must remain legible and **implement Dynamic Type + Bold Text accessibility** (via Apple Unity plug-ins or manual scaling).
- **Supporting Dynamic Type:** layout must adapt to all sizes; increase meaningful icons with font; minimize truncation; consider stacking columns at large sizes; maintain hierarchy at large sizes.
- iOS text-style sizes at **Large (default)** verbatim: `Large Title 34/41, Title1 28/34, Title2 22/28, Title3 20/24, Headline 17/22 Semibold, Body 17/22 Regular, Callout 16/21, Subhead 15/20, Footnote 13/18, Caption1 12/16, Caption2 11/13`. Larger Accessibility sizes go to **AX1-AX5** (Body up to 28pt).
- `Map:` `ThemeData.textTheme` from `ColorScheme` only; respect `MediaQuery.textScaler.of(context)` (never hard `fontSize`). `textTheme.titleLarge` for app-bar headline, `labelLarge` for buttons, `bodyMedium` for lists. Tabular figures via `FontFeature.tabularFigures()` for machine data; mono (`ui-monospace`/`Roboto Mono`) only for addresses/hashes/code.

---

## 7. MATERIALS (HIG: Materials — Liquid Glass, Standard materials; iOS/MacOS sections)

Apple's rules, faithfully:

- Liquid Glass forms a **distinct functional layer** for controls/navigation (tab bars, sidebars) **floating above content layer**. Never in content layer (exception: transient sliders/toggles when activated).
- **Use sparingly** — system components get it automatically; limit custom Liquid Glass to most important functional elements. Overuse distracts from content.
- Two variants: **`regular`** (blurred/luminosity-adjusted, with scroll-edge effect — default for text-heavy alerts/sidebars/popovers) vs **`clear`** (highly translucent for media backgrounds). For `clear` over bright content, add 35% dark dimming layer per `glass/clear`.
- Standard materials + vibrancy convey structure **beneath** glass. Choose by **semantic meaning, never by apparent color**; use vibrant colors on top for legibility. Thickness matters: `ultraThin/thin` more translucent (context), `regular` default, `thick` more opaque (contrast).
- Project override (taste, not Apple): **no Liquid Glass anywhere in this app** — no refraction, no specular highlights, no morphing glass. Classic pre-glass frost is welcome on macOS; on **Android the equivalent is Material `Surface` + tonal elevation**, not glass blur. See `AGENTS.md:36-41`.

- `Map:` No `NSVisualEffectView`. Use `Surface`, `Card`, `AppBar.scrolledUnderElevation`, `surfaceContainer` tonal steps for hierarchy. `BackdropFilter` blur **only** as transient scrim behind sheets/dialogs, with solid-fill fallback under `disableAnimations`/`prefers-reduced-transparency`. Never tint multiple elevated surfaces.

---

## 8. DARK MODE (HIG: Dark Mode — iOS section)

- **Never offer an app-specific appearance setting.** It doubles work and reads as broken.
- Must look good in **light, dark, and Auto** (can switch mid-session).
- **Avoid hard-coded/inflexible colors.** Use semantic colors (`label` ramp, system backgrounds) that adapt; custom colors via Color Set assets with bright/dim variants.
- Aim for **contrast ≥4.5:1** (minimum), 7:1 for small custom text.
- Soften pure-white imagery so it doesn't glow.
- Labels: use system `label/secondaryLabel/tertiaryLabel/quaternaryLabel`; text fields use system views so vibrancy applies.
- iOS Dark uses **base vs elevated** backgrounds — base dimmer (background interfaces recede), elevated brighter (foreground popovers/sheets advance); system auto-switches elevated when view is foreground/multitasking needs.
- `Map:` `ThemeMode.system` + `theme:` (seed light) / `darkTheme:` (seed `brightness:dark`) only. No persisted `[data-theme]` toggle. Listen to `platformBrightness` only if manual work needed; prefer framework. Screenshot both modes.

---

## 9. BUTTONS (HIG: Buttons — Best practices, Style, Content, Role — iOS section)

- Minimum hit region **44x44pt** on iOS (Android touch target is 48x48dp) — always keep tappable; space so distinct.
- **Always include press state** on custom buttons.
- **Prominent style for most likely action** — accent background draws eye. **One or two prominent per view max** — more is cognitive load. **Use style, not size**, to mark preferred choice.
- Content: symbol, text label, or both. Associate familiar actions with **standard symbols**; write text labels as **short verb-led title-style phrases** ("Add to Cart").
- Roles: Normal, **Primary** (default — answers Return, accent), **Cancel**, **Destructive** (system red). Never assign Primary to destructive even if most likely.
- iOS activity feedback: **configure button to display activity indicator inside** with label swap (`Checkout` → `Checking out…`) next to label, hiding image while spinning.
- macOS-specific types **do not apply to Android**: push/flexiblePush with ellipsis, `smallSquare` gradient, help `?` button, image button 10px padding — ignore for Flutter; equivalent is Material `Filled/Tonal/Outlined/TextButton` + FAB.
- `Map:` prominent = `FilledButton` accent fill + `onPrimary` text, 6-20dp radius (repo 14), disabled 38% opacity (M3) / 50% (mock), ripple/press state, inline 12-18dp spinner + label swap for busy. Secondary = `FilledButton.tonal`; tertiary = `Outlined/TextButton`. Destructive confirm = `FilledButton` with `error` fill + plain Keep/Cancel escape. From `ThemeData.filledButtonTheme` etc., never inline hex.

---

## 10. ALERTS, ACTION SHEETS, SHEETS (HIG: Alerts, Action Sheets, Sheets — iOS sections)

- **Use alerts sparingly** — each interrupts. Every alert must carry essential info + useful actions.
- **Never alert merely to inform** — use inline contextual indicator (Mail pattern). Never for common undoable deletions; never at launch (show cached content + nonintrusive label).
- Alerts: **title 1-2 lines, specific** — never `Error` or code. Sentence-case for sentences, title-case for fragments. Informative text only if adds value, short complete sentences. **Never explain buttons in body**.
- Buttons: **one/two-word verb titles** (`Erase`, `Convert`, `Ignore`); informational `OK`; cancellable offers `Cancel` (never default). Placement: default/most likely **trailing or top**; Cancel **leading or bottom**. Destructive styling **only** for unintended destructive consequences — deliberately chosen `Empty Trash` keeps plain title so Return confirms intent. If destructive, include `Cancel` safe escape; to discourage auto-Return, make **no** button default.
- **Action sheet vs alert vs menu:** use **action sheet** to offer **choices related to an intentional action** (e.g. Mail cancel → Delete/Save draft) — not alert. Action sheet title single line, message only if necessary; **destructive at top**, Cancel at bottom. Don't let it scroll; max ~3 choices + Cancel.
- **Sheet:** modal (blocks parent) or **nonmodal on iOS** (Notes format sheet coexists with editing). Show only **one sheet at a time**; close first before showing second. For single-view sheets: Cancel leading, Done trailing; multi-step: first step Cancel leading + Done inactive trailing, subsequent steps Back navigation. Support **detents** (large full, medium half) + **grabber** (`prefersGrabberVisible`) + swipe-to-dismiss (confirm with action sheet if unsaved).
- `Map:` version gates `error/UPDATE_REQUIRED` render as **inline rows** with canonical verbatim `"Update FuseItAll on <device> to build >= N"` (never a modal alert), following informative-text rules. `AlertDialog` (or fullscreen `Dialog` for complex flows) with `TextButton` actions; selectable errors via `SelectableText.rich`, not `SnackBar` alone.

---

## 11. PROGRESS INDICATORS (HIG: Progress Indicators — iOS section)

- Two kinds: **determinate** (known duration: bars/circular tracks filling) vs **indeterminate** (unknown: spinners + iOS indeterminate bar).
- **Prefer determinate** when knowable; report honestly; don't park at 90%.
- **Keep moving** — stationary reads as stalled → show explanatory feedback.
- **Don't switch shapes mid-task** (spinner ↔ bar); keep location consistent.
- Descriptions accurate/specific — never `Loading…` / `Authenticating…`.
- Offer **Cancel where safe** (plus Pause where partial progress matters); warn when halting destroys progress.
- iOS refresh: **refresh control** is specialized activity indicator hidden until drag-down; perform automatic updates; title only if adds value (e.g. last update time).
- `Map:` `LinearProgressIndicator` at top for page loads, `CircularProgressIndicator` 12-20dp beside initiating button/status line, border-rotation on `transform` only, killed under `MediaQuery.disableAnimations` (`prefers-reduced-motion`). No perpetual ambient loops.

---

## 12. MENUS AND CONTEXT MENUS (HIG: Menus, Context Menus — iOS section)

- Menus: small, strictly relevant. **Provide either context menu OR edit menu for an item, not both.**
- **Hide unavailable items; never dim them** (Cut/Copy/Paste exempted may show disabled). Every context item must also exist in main UI — menus shortcut, never warehouse.
- Be consistent: if some same-kind items have menus, **all** do.
- At most **one submenu level** with predictive title; ~three separator groups max; no keyboard shortcuts shown (menu itself is shortcut).
- Labels: short action descriptions; title only when clarifies scope (selection count).
- Use **same icons as system** for Copy/Share/Delete. Medium/small layouts: medium shows 3 labeled icons on top row (Notes: Scan/Lock/Pin), small shows 4 icon-only — use only for closely related grouped actions (Bold/Italic/Underline/Strike).
- Context menus on iOS: **graphical preview** (condensed actual content) that animates with dimmed behind; keep clipping path matching preview shape. Destructive items **last, red via `destructive` attribute**.
- `Map:` long-press → `MenuAnchor` / `PopupMenuButton` (overflow) or `showModalBottomSheet` (2-4 frequent actions). One component for all same-kind targets, clamps to viewport, dismisses on back/scrim tap/scroll/resize. Destructive confirms **inline 2-step**, never second modal. Menu suppressed is not needed on Android (no native webview menu to suppress — use single component).

---

## 13. ICONS AND SF SYMBOLS (HIG: Icons, SF Symbols — Standard icons, Rendering modes, Weights/scales)

- Icons convey objects/concepts in toolbars, lists, menus, inline text. Design as **recognizable, highly simplified** shapes; maintain **consistent size/detail/weight/perspective** across all icons; adjust individual size for visual weight.
- **Match symbol weight to adjacent text weight**; pick scale (small/medium/large) for emphasis without breaking match. **Outline default** for toolbars/lists; **fill where selection/emphasis** needed. **Monochrome default**; hierarchical/palette/multicolor only with intent. **Animate only to communicate** (feedback/status/progress), never ambience.
- Never use symbols (or confusing lookalikes) in app icons/logos/trademarks. Never use replicas of Apple hardware.
- Standard icons list (use familiar pairings): `scissors` Cut, `document.on.document` Copy, `document.on.clipboard` Paste, `checkmark` Done/Save, `xmark` Cancel/Close, `trash` Delete, `arrow.uturn.backward` Undo, `square.and.pencil` Compose, `magnifyingglass` Search, `line.3.horizontal.decrease` Filter, `square.and.arrow.up` Share, `person.crop.circle` Account, etc.
- `Map:` **Flutter has no SF Symbols font** — compliant fallback is **single Material family** for whole project (`Icons.*` or `material_symbols_icons` for variable weight/optical size). Never mix `CupertinoIcons` + `Icons` + third pack. Never hand-draw glyph paths; if glyph unavailable, use **short verb-led text label** per HIG. Monochrome/weight-matched is the only token.

---

## 14. ACCESSIBILITY AND MOTION (HIG: Accessibility, Motion; M3: Motion)

- Reach larger audience: **Intuitive, Perceivable, Adaptable**. Audit with `Accessibility Inspector`-equivalent.
- Vision: support **≥200% text enlargement** (140% watchOS), use system type ramp, avoid Thin, contrast **≥4.5:1 up to 17pt, 3:1 at 18pt/bold**, prefer system colors (auto Increase Contrast variants), never color-only signalling.
- Mobility: controls **≥44x44pt hit** (min 28), **12pt padding** with bezel / **24pt without bezel**; **simplest gesture for frequent actions**; offer alternative to gestures (button for swipe-to-dismiss); support Voice Control / Siri shortcuts.
- Speech/Cognitive: support **Full Keyboard Access**, Switch Control; keep actions **simple/intuitive**; avoid time-boxed auto-dismiss; in games offer difficulty accommodation; reduce fast/blinking animations when **Reduce Motion** active — tighten springs, track directly with gesture, avoid z-layer depth animation, replace x/y/z transitions with fades, avoid blur animation.
- `Map:` `MediaQuery.disableAnimations` / `textScaler` at 100% and 200% must not overflow; `Semantics(label:)` + `tooltip` on every `IconButton`; `AnimatedContainer/AnimatedSwitcher` 150-300ms `Easing.emphasized` via `transform`/`opacity` only, each justifiable in one sentence; never unconstrained loops.

---

## 15. PRE-FLIGHT CHECK

Run every box before shipping a screen. One failure means not done.

- [ ] HIG Read declared (§0)?
- [ ] Screen kind correct (primary vs sheet/action-sheet/dialog), bottom/middle reachable area, `SafeArea` respected, rotation/fold tested?
- [ ] Tab bar visible, labels present, 3-5 items max (no More overflow), badges only for critical, sidebar only on expanded width?
- [ ] Toolbar anatomy leading/center/trailing ≤3 groups, title ≤15 chars (never app name), one prominent action trailing?
- [ ] Colors all semantic (label/secondaryLabel/separator/primary Roles), light+dark+increasedContrast, no double-meaning, ≥4.5:1?
- [ ] Typography on iOS table (Body 17 default, 11 floor, no light weights), Dynamic Type scaling tested 100%/200%, mono only for machine data?
- [ ] Materials per §7 (functional layer only, `regular` vs `clear` +35% dimming correctly, no content-layer glass)?
- [ ] Appearance follows system (`ThemeMode.system`, `theme`/`darkTheme`), base vs elevated dark backgrounds not hard-coded?
- [ ] Buttons: 44→48dp hit, 12/24pt padding, one/two prominent max, verb labels, press state, activity indicator + label swap, destructive never primary?
- [ ] Alerts/action-sheets/sheets: only for decisions, inline rows for info, specific 1-2 line titles, verb buttons, Cancel never default, one sheet at a time, grabber/detents where resizable?
- [ ] Progress honest, determinate preferred, consistent location, `refreshControl`-style where applicable, spinners 12-20dp unlabeled, `disableAnimations` safe?
- [ ] Menus/context small/relevant/consistent, preview present where needed, unavailable hidden (Cut/Copy/Paste exempt), destructive last/red, submenu ≤1 level, all items mirrored in main UI?
- [ ] Single icon family (Material via `Icons.*`), weight matched, filled only for selection, no hand-drawn glyphs, text label fallback?
- [ ] Zero em-dashes in visible copy?
- [ ] Motion `transform`/`opacity` only, each justifiable in one sentence, Reduce Motion handled, semantics/tooltips present, large-type overflow tested?
