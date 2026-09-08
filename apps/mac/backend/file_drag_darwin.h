// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

#ifndef FILE_DRAG_DARWIN_H
#define FILE_DRAG_DARWIN_H

#include <stdint.h>

int nativeStartFileDrag(uintptr_t svcPtr, const char *remotePathC, const char *filenameC, int64_t fileSize);

#endif /* FILE_DRAG_DARWIN_H */
