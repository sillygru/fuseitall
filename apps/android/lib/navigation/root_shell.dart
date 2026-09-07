// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Reading this as: primary navigation shell for paired state, following HIG 1/2/3/4/8/13.

import 'package:flutter/material.dart';

class RootShell extends StatefulWidget {
  const RootShell({
    required this.home,
    required this.devices,
    required this.settings,
    this.initialIndex = 0,
    super.key,
  });

  final Widget home;
  final Widget devices;
  final Widget settings;
  final int initialIndex;

  @override
  State<RootShell> createState() => _RootShellState();
}

class _RootShellState extends State<RootShell> {
  late int _index;

  @override
  void initState() {
    super.initState();
    _index = widget.initialIndex;
  }

  @override
  Widget build(BuildContext context) {
    final pages = [widget.home, widget.devices, widget.settings];
    return LayoutBuilder(
      builder: (context, constraints) {
        final wide = constraints.maxWidth >= 600;
        final expanded = constraints.maxWidth >= 840;
        if (expanded) {
          return Scaffold(
            body: Row(
              children: [
                NavigationDrawer(
                  selectedIndex: _index,
                  onDestinationSelected: (i) => setState(() => _index = i),
                  children: const [
                    Padding(
                      padding: EdgeInsets.fromLTRB(28, 16, 16, 8),
                      child: Text('FuseItAll'),
                    ),
                    NavigationDrawerDestination(
                      icon: Icon(Icons.home_outlined),
                      selectedIcon: Icon(Icons.home),
                      label: Text('Home'),
                    ),
                    NavigationDrawerDestination(
                      icon: Icon(Icons.devices_outlined),
                      selectedIcon: Icon(Icons.devices),
                      label: Text('Devices'),
                    ),
                    NavigationDrawerDestination(
                      icon: Icon(Icons.settings_outlined),
                      selectedIcon: Icon(Icons.settings),
                      label: Text('Settings'),
                    ),
                  ],
                ),
                const VerticalDivider(width: 1),
                Expanded(child: _bodyWithAppBar(pages[_index])),
              ],
            ),
          );
        }
        if (wide) {
          return Scaffold(
            body: Row(
              children: [
                NavigationRail(
                  selectedIndex: _index,
                  onDestinationSelected: (i) => setState(() => _index = i),
                  labelType: NavigationRailLabelType.all,
                  destinations: const [
                    NavigationRailDestination(
                      icon: Icon(Icons.home_outlined),
                      selectedIcon: Icon(Icons.home),
                      label: Text('Home'),
                    ),
                    NavigationRailDestination(
                      icon: Icon(Icons.devices_outlined),
                      selectedIcon: Icon(Icons.devices),
                      label: Text('Devices'),
                    ),
                    NavigationRailDestination(
                      icon: Icon(Icons.settings_outlined),
                      selectedIcon: Icon(Icons.settings),
                      label: Text('Settings'),
                    ),
                  ],
                ),
                const VerticalDivider(width: 1),
                Expanded(child: _bodyWithAppBar(pages[_index])),
              ],
            ),
          );
        }
        return Scaffold(
          appBar: AppBar(title: const Text('Paired'), scrolledUnderElevation: 2),
          body: SafeArea(child: pages[_index]),
          bottomNavigationBar: NavigationBar(
            selectedIndex: _index,
            onDestinationSelected: (i) => setState(() => _index = i),
            destinations: const [
              NavigationDestination(
                icon: Icon(Icons.home_outlined),
                selectedIcon: Icon(Icons.home),
                label: 'Home',
              ),
              NavigationDestination(
                icon: Icon(Icons.devices_outlined),
                selectedIcon: Icon(Icons.devices),
                label: 'Devices',
              ),
              NavigationDestination(
                icon: Icon(Icons.settings_outlined),
                selectedIcon: Icon(Icons.settings),
                label: 'Settings',
              ),
            ],
          ),
        );
      },
    );
  }

  Widget _bodyWithAppBar(Widget child) => Column(
        children: [
          AppBar(title: const Text('Paired'), scrolledUnderElevation: 2),
          Expanded(child: SafeArea(child: child)),
        ],
      );
}
