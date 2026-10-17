<?php
// sleep/usleep and max_execution_time.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/runtime/timing');
header('Content-Type: text/plain');

switch ($q['op'] ?? '') {
    case 'usleep':
        $t = hrtime(true);
        usleep((int) ($q['ms'] ?? 0) * 1000);
        $ms = intdiv(hrtime(true) - $t, 1000000);
        echo $ms >= (int) ($q['ms'] ?? 0) ? 'slept' : "short ($ms ms)";
        break;
    case 'sleep':
        echo sleep((int) ($q['s'] ?? 0));
        break;
    case 'busy':
        // CPU time counts against max_execution_time.
        echo 'start;';
        $end = microtime(true) + (int) ($q['s'] ?? 0);
        while (microtime(true) < $end) {
        }
        echo 'end';
        break;
    default:
        http_response_code(400);
        echo 'unknown op';
}
