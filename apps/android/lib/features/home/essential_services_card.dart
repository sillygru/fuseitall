// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Reading this as: inline permissions status for paired home, following HIG 5/6/9.

import 'package:flutter/material.dart';

import '../../widgets/haptics.dart';
import '../../widgets/status_pill.dart';
import '../permissions/permissions.dart';

class EssentialServicesCard extends StatelessWidget {
  const EssentialServicesCard({
    required this.status,
    required this.permissions,
    super.key,
  });

  final PermissionStatus? status;
  final Permissions permissions;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final isDark = scheme.brightness == Brightness.dark;

    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  Icons.settings_suggest_outlined,
                  size: 20,
                  color: scheme.onSurface,
                ),
                const SizedBox(width: 8),
                Text(
                  'Essential Services',
                  style: Theme.of(context).textTheme.titleSmall?.copyWith(
                        fontWeight: FontWeight.w700,
                        letterSpacing: -0.2,
                      ),
                ),
              ],
            ),
            const SizedBox(height: 12),
            _row(
              context,
              icon: Icons.notifications_outlined,
              title: 'Notification access',
              body: status == null
                  ? 'Checking…'
                  : status!.listenerEnabled
                      ? 'Enabled — phone notifications mirror to Mac.'
                      : 'Disabled — mirroring is paused until you enable it.',
              enabled: status?.listenerEnabled ?? false,
              onFix: permissions.openListenerSettings,
            ),
            _divider(isDark),
            _row(
              context,
              icon: Icons.qr_code_2_outlined,
              title: 'Camera (pairing only)',
              body: 'Used once to scan the Mac code. Nothing is recorded.',
              enabled: true,
              onFix: null,
            ),
            _divider(isDark),
            _row(
              context,
              icon: Icons.folder_outlined,
              title: 'All files access',
              body: status == null
                  ? 'Checking…'
                  : status!.allFilesAccessGranted
                      ? 'Granted — Mac can browse full storage.'
                      : 'Needed for File Manager to see storage. Grants full access (GitHub sideload).',
              enabled: status?.allFilesAccessGranted ?? false,
              onFix: status?.allFilesAccessGranted == true ? null : permissions.openAllFilesAccessSettings,
            ),
            _divider(isDark),
            _row(
              context,
              icon: Icons.photo_library_outlined,
              title: 'Photos access',
              body: status == null
                  ? 'Checking…'
                  : status!.photosGranted
                      ? 'Granted — Mac can browse the photo library.'
                      : status!.photosLimited
                          ? 'Limited — only selected photos are visible to Mac. Tap Enable to allow more.'
                          : 'Needed for Photos to show the library. Scoped to images and video only.',
              enabled: status?.photosGranted ?? false,
              onFix: (status?.photosGranted ?? false) ? null : permissions.requestPhotosPermission,
            ),
            _divider(isDark),
            _row(
              context,
              icon: Icons.contacts_outlined,
              title: 'Contacts access',
              body: status == null
                  ? 'Checking…'
                  : status!.contactsGranted
                      ? 'Granted — Mac can browse and search your phone contacts.'
                      : 'Needed for Mac to browse and search your contacts.',
              enabled: status?.contactsGranted ?? false,
              onFix: (status?.contactsGranted ?? false) ? null : permissions.requestContactsPermission,
            ),
            _divider(isDark),
            _row(
              context,
              icon: Icons.sms_outlined,
              title: 'SMS access',
              body: status == null
                  ? 'Checking…'
                  : status!.smsGranted
                      ? 'Granted — Mac can sync conversation threads and send SMS.'
                      : 'Needed for Mac to sync message threads and send SMS.',
              enabled: status?.smsGranted ?? false,
              onFix: (status?.smsGranted ?? false) ? null : permissions.requestSmsPermission,
            ),
            if (status != null && !status!.listenerEnabled) ...[
              const SizedBox(height: 10),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                decoration: BoxDecoration(
                  color: scheme.surfaceContainerHighest,
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Row(
                  children: [
                    Icon(Icons.lightbulb_outline, size: 16, color: scheme.onSurfaceVariant),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        'Tip: enable notification access, then tap Reconnect.',
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                              color: scheme.onSurfaceVariant,
                            ),
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }

  Widget _divider(bool isDark) => Padding(
        padding: const EdgeInsets.symmetric(vertical: 8),
        child: Divider(
          color: isDark ? const Color(0x18FFFFFF) : const Color(0x0F000000),
          height: 1,
          thickness: 0.5,
        ),
      );

  Widget _row(
    BuildContext context, {
    required IconData icon,
    required String title,
    required String body,
    required bool enabled,
    required VoidCallback? onFix,
  }) {
    final scheme = Theme.of(context).colorScheme;
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            padding: const EdgeInsets.all(8),
            decoration: BoxDecoration(
              color: scheme.surfaceContainerHighest,
              borderRadius: BorderRadius.circular(12),
            ),
            child: Icon(icon, color: scheme.onSurfaceVariant, size: 20),
          ),
          const SizedBox(width: 14),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Expanded(
                      child: Text(
                        title,
                        style: Theme.of(context).textTheme.titleSmall?.copyWith(
                              fontWeight: FontWeight.w700,
                              letterSpacing: -0.2,
                            ),
                      ),
                    ),
                    StatusPill(enabled: enabled),
                  ],
                ),
                const SizedBox(height: 4),
                SelectableText(
                  body,
                  style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                        color: scheme.onSurfaceVariant,
                        fontSize: 13,
                        height: 1.35,
                      ),
                ),
                if (onFix != null && !enabled) ...[
                  const SizedBox(height: 8),
                  FilledButton.tonal(
                    onPressed: () {
                      AppHaptics.light();
                      onFix();
                    },
                    style: FilledButton.styleFrom(
                      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                      minimumSize: const Size(0, 36),
                      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
                    ),
                    child: const Text('Enable'),
                  ),
                ],
              ],
            ),
          ),
        ],
      ),
    );
  }
}
