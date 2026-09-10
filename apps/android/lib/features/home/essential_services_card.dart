// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Reading this as: inline permissions status for paired home, following HIG 5/6/9.

import 'package:flutter/material.dart';

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
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(Icons.settings_suggest_outlined, size: 20),
                const SizedBox(width: 6),
                Text(
                  'Essential Services',
                  style: Theme.of(context).textTheme.titleSmall?.copyWith(
                        fontWeight: FontWeight.bold,
                      ),
                ),
              ],
            ),
            const SizedBox(height: 8),
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
            _row(
              context,
              icon: Icons.qr_code_2_outlined,
              title: 'Camera (pairing only)',
              body: 'Used once to scan the Mac code. Nothing is recorded.',
              enabled: true,
              onFix: null,
            ),
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
              const SizedBox(height: 4),
              Text(
                'Tip: enable notification access, then tap Reconnect.',
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: scheme.onSurfaceVariant,
                    ),
              ),
            ],
          ],
        ),
      ),
    );
  }

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
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, color: scheme.onSurfaceVariant),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  title,
                  style: Theme.of(context).textTheme.titleSmall?.copyWith(
                        fontWeight: FontWeight.w600,
                      ),
                ),
                const SizedBox(height: 4),
                StatusPill(enabled: enabled),
                const SizedBox(height: 4),
                SelectableText(
                  body,
                  style: Theme.of(context).textTheme.bodyMedium,
                ),
                if (onFix != null && !enabled) ...[
                  const SizedBox(height: 6),
                  FilledButton.tonal(
                    onPressed: onFix,
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
