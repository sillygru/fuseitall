<!--
  SPDX-License-Identifier: AGPL-3.0-only

  Status pill: a dot plus a short label on a tinted badge. Color never
  travels alone: the label always names the state (Online, Seen 5m ago).
-->
<script lang="ts">
  interface Props {
    kind: 'ok' | 'warn' | 'bad' | 'neutral';
    label: string;
  }

  let { kind, label }: Props = $props();
</script>

<span class="pill pill-{kind}">
  <span class="pill-dot" aria-hidden="true"></span>
  <span>{label}</span>
</span>
