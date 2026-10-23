---
title: Install
description: Run Kaasu on your own machine in a few minutes.
sidebar:
  order: 1
---

You need a computer that stays on during shop hours. A small cloud server, a Mac or a Windows PC all work.

## Try it with demo data

```bash
git clone https://github.com/opensawce/kaasu
cd kaasu && make dev
```

Everything runs offline with sample orders. Open the address shown in the terminal.

## Send a pretend bank alert

```bash
kaasu sim exact-match
```

An order turns green. List more scenarios with `kaasu sim --list`.

## Check the ledger

```bash
kaasu verify
```

A healthy result reads "Ledger intact". Any edit to history makes this check fail.
