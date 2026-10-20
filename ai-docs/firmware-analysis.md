# Firmware Analysis

Notes from reading the keyboard firmware in Ghidra (2026-10-20), mainly to
understand realtime lighting (`04 20`), color readback (`04 F5`) and when
settings are written to flash. See [hid-protocol.md](hid-protocol.md) for the
protocol itself.

## The Image

Aula's site offers "F108Pro Firmware" as `F108PRO firmware.exe` (aid 561 on
aulastar.com/drive). It is a Sonix ISP flasher with a ZIP appended to the
executable; the ZIP holds `Settings\FWFiles\SN32F290.hex`, an unencrypted
Intel HEX image (92 KB of code at `0x0`-`0x167F3`, plus a word at `0x3FFFC`).

* MCU: **Sonix SN32F290** (Cortex-M0, 48 MHz). Vector table: initial SP
  `0x20002B18`, reset handler `0x7EE5`.
* Load it in Ghidra as raw binary, `ARM:LE:32:Cortex`, base `0`, with RAM at
  `0x20000000` and peripherals at `0x40000000`. Most handlers are reached
  only through jump tables (`__ARM_common_switch8` at `0x8E78`), so also
  create functions at every `push {..., lr}` (`B5xx`) in undefined code.

**It is not the build on the keyboard.** Three of the four HID report
descriptors and the configuration descriptor match the keyboard byte for byte,
and every behavior below matched hardware tests. But the image says
`0C45:8801`, `bcdDevice 0x0100`, strings "SONiX" / "USB KEYBOARD", and its
boot keyboard descriptor ends in `2A 65 00` where the keyboard's (`0C45:800A`,
`bcdDevice 0x0113`, "AULA F108Pro") ends in `2A FF 00`. Do not flash it: the
keyboard would come back as a different device. Patching the firmware would
need a dump of the actual build first.

## Memory Map (as found)

| Address        | Contents                                                |
| -------------- | ------------------------------------------------------- |
| `0x9000`       | Flash page erased by `04 17` (function settings)         |
| `0x9400`       | Flash page erased by `04 11` (key remap)                 |
| `0x9800`       | Flash page erased by `04 13` (lighting mode)             |
| `0x9C00`       | Flash page erased by `04 23` (per-key colors)            |
| `0x200000AC`   | USB command state (below)                                |
| `0x200012F0`   | EP0 buffer; the 64-byte report starts at `+0x14`         |
| `0x20001782`   | LED PWM levels, R (`u16` per LED)                        |
| `0x200018A8`   | LED PWM levels, G                                        |
| `0x200019CE`   | LED PWM levels, B                                        |
| `0x20001AF4`.. | Effect color buffers that the renderer copies to PWM    |
| `0x126E9`      | Table: light index -> PWM position                       |

USB command state at `0x200000AC`:

| Offset | Meaning                                                         |
| ------ | --------------------------------------------------------------- |
| `+1`   | Data phase type: 0 command, 1 `04 F5` readback, 2 realtime/flash data, 3 wait for `04 02`, 4 `04 AB`, 5 LCD data |
| `+3`   | Realtime flag (set by `04 20`)                                   |
| `+5`   | Realtime timer, 100 ms ticks (`0x32` = 5 s)                      |
| `+10`  | Data reports still expected (from byte[8] of the command)        |

## Functions

| Address  | Role |
| -------- | ---- |
| `0x06D6` | `main`: init, then a loop calling the tick handler, the lighting dispatcher and the LCD tasks |
| `0x0444` | Tick handler. Every 50 ticks (100 ms) it decrements the realtime timer and clears the realtime flag when it reaches 0 |
| `0x39A4` | `04 xx` command handler on SET_REPORT: `04 11/13/17/23` erase their flash page (`FUN_7B28`), `04 20` arms the 5 s timer, `04 F0` calls `FUN_D984`, `04 18`/`04 19` clear/set a state bit |
| `0x3B44` | Runs on the GET_REPORT after a command and sets up the data phase for it (type, expected report count from byte[8]). This is why the readback is required |
| `0x3DA6` | Data phase dispatcher, jump table on the type at `+1` |
| `0x37D0` | Type 2 data. With the realtime flag: 16 `[index R G B]` entries straight into the PWM arrays. Without it: data is programmed to flash (`FUN_1E38`), erasing a page at every 1 KB boundary |
| `0x38CC` | Type 1: `04 F5` readback, 16 entries per GET from the PWM arrays |
| `0x36E0` | Type 3: accepts only `04 02` (replies with a checksum), then returns to type 0; anything else gets `ff` |
| `0x3DE6` | Resets the data phase after 2000 ticks without traffic |
| `0xAD42` | Copies the effect buffers to the PWM arrays, but not while the realtime flag is set, which is why animations pause during realtime frames |
| `0xD606` | Lighting dispatcher: jump table at `0xD636` over the effect renderers (`0xBE22`-`0xD4D6`) |
| `0x4A18` | Link to the 2.4G radio module (frames with codes `A1`-`A6`) |

## Patches Considered (Not Done)

Both would be small, but need the keyboard's own firmware build:

* Host-controlled realtime timeout, including "until cleared": replace the
  `strb r2, [r4, #5]` at `0x3B28` with a `bl` to a few instructions that
  take the timeout from byte[9] of the `04 20` header.
* Per-key overlay instead of a global flag, so animations keep running on
  the other keys: a mask set by `0x37D0` and checked per LED in `0xAD42`.
