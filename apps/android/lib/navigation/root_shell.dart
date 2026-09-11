// SPDX-License-Identifier: AGPL-3.0-only

// Reading this as: primary navigation shell for paired state, following HIG 1/2/3/4/8/13
// with tactile micro-interactions and smooth page transitions.

import 'package:flutter/material.dart';

import '../widgets/haptics.dart';

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

  void _onSelect(int index) {
    if (_index == index) return;
    AppHaptics.selection();
    setState(() => _index = index);
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
                  onDestinationSelected: _onSelect,
                  children: const [
                    Padding(
                      padding: EdgeInsets.fromLTRB(28, 20, 16, 12),
                      child: Text(
                        'FuseItAll',
                        style: TextStyle(
                          fontSize: 18,
                          fontWeight: FontWeight.w700,
                          letterSpacing: -0.3,
                        ),
                      ),
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
                  onDestinationSelected: _onSelect,
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
          appBar: AppBar(
            title: const Text('Paired'),
            scrolledUnderElevation: 0,
          ),
          body: SafeArea(
            child: AnimatedSwitcher(
              duration: const Duration(milliseconds: 200),
              switchInCurve: Curves.easeOutCubic,
              switchOutCurve: Curves.easeInCubic,
              child: KeyedSubtree(
                key: ValueKey<int>(_index),
                child: pages[_index],
              ),
            ),
          ),
          bottomNavigationBar: NavigationBar(
            selectedIndex: _index,
            onDestinationSelected: _onSelect,
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
          AppBar(
            title: const Text('Paired'),
            scrolledUnderElevation: 0,
          ),
          Expanded(
            child: SafeArea(
              child: AnimatedSwitcher(
                duration: const Duration(milliseconds: 200),
                switchInCurve: Curves.easeOutCubic,
                switchOutCurve: Curves.easeInCubic,
                child: KeyedSubtree(
                  key: ValueKey<int>(_index),
                  child: child,
                ),
              ),
            ),
          ),
        ],
      );
}
