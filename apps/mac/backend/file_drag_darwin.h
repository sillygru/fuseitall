// SPDX-License-Identifier: AGPL-3.0-only

#ifndef FILE_DRAG_DARWIN_H
#define FILE_DRAG_DARWIN_H

#include <stdint.h>

int nativeStartFileDrag(uintptr_t svcPtr, const char *remotePathC, const char *filenameC, int64_t fileSize);

#endif /* FILE_DRAG_DARWIN_H */
