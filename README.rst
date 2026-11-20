.. SPDX-License-Identifier: GPL-2.0-only
.. Copyright (C) 2026, Shu De Zheng <imchuncai@gmail.com>. All Rights Reserved.

==============
Umem-cache-ctl
==============

Umem-cache-ctl is a CLI tool for managing and operating
`Umem-cache <https://github.com/imchuncai/umem-cache>`_.

Usage
=====
::

	go run . -h

	Usage:
	  umem-cache-ctl [-tls_key <path> -tls_cert <path> -tls_ca_cert <path>]
	                 [-timeout <seconds>] <init|change|cluster|stat|stats|warmup>
			 <address...>

	Example:
	  umem-cache-ctl stat [::1]:10047

	WARNING:
	  The warmup command is stupid, don't use it unless you know what you're doing.
