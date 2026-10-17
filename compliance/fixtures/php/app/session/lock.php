<?php
// Appends a tag to the session while holding (or releasing) the session lock.
// delay_ms runs before session_start() so a parallel request reliably comes second.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/session/lock');

usleep((int) ($q['delay_ms'] ?? 0) * 1000);
if (($q['ro'] ?? '0') === '1') {
    session_start(['read_and_close' => true]);
} else {
    session_start();
}
$_SESSION['seen'][] = $q['tag'] ?? '?';
$seen = implode(',', $_SESSION['seen']);
if (($q['close'] ?? '0') === '1') {
    session_write_close();
}
usleep((int) ($q['hold_ms'] ?? 0) * 1000);
echo ($q['tag'] ?? '?') . ' seen=' . $seen;
