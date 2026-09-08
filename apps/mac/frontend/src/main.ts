// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.
import { mount } from 'svelte'
import './app.css'
import './motion.css'
import App from './App.svelte'

mount(App, { target: document.getElementById('app')! })
