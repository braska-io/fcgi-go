<?php
// flock() on a shared file in the state directory, across children.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/runtime/lock');
header('Content-Type: text/plain');

$file = getenv('FIXTURE_STATE') . '/rt-lock';
$pre = (int) ($q['pre_ms'] ?? 0);
$hold = (int) ($q['hold_ms'] ?? 0);
if ($pre > 0) {
    usleep($pre * 1000);
}
$h = fopen($file, 'c');
switch ($q['op'] ?? '') {
    case 'hold':
        flock($h, LOCK_EX);
        usleep($hold * 1000);
        flock($h, LOCK_UN);
        echo 'held';
        break;
    case 'wait':
        $t = hrtime(true);
        flock($h, LOCK_EX);
        $waited = intdiv(hrtime(true) - $t, 1000000);
        flock($h, LOCK_UN);
        echo $waited >= (int) ($q['min_ms'] ?? 0) ? 'waited' : "no wait ($waited ms)";
        break;
    case 'try':
        $wouldBlock = 0;
        $ok = flock($h, LOCK_EX | LOCK_NB, $wouldBlock);
        echo $ok ? 'acquired' : ($wouldBlock ? 'would block' : 'failed');
        if ($ok) {
            flock($h, LOCK_UN);
        }
        break;
    default:
        http_response_code(400);
        echo 'unknown op';
}
fclose($h);
