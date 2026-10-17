<?php
// set_time_limit() and max_execution_time (CPU time under php-fpm NTS).
//   case=ini                  ini_get('max_execution_time')
//   case=cpu&k=K              set_time_limit(1), burn 3 s CPU; a shutdown
//                             function marks connection_status()
//   case=default&k=K          burn 8 s CPU against max_execution_time = 5
//   case=sleep                set_time_limit(1), sleep 2 s, "survived"
//   case=restart              set_time_limit(1) twice around 0.7 s CPU each
//   case=zero                 set_time_limit(0), burn 1.5 s CPU
require __DIR__ . '/../../_fixture.php';
require __DIR__ . '/../pdo/_db.php';
$q = fixture('app/lifecycle/timelimit');
$k = $q['k'] ?? '';

function mark_status(string $k): void
{
    register_shutdown_function(function () use ($k) {
        mark($k, 'shutdown status=' . connection_status());
    });
}

switch ($q['case'] ?? '') {
case 'ini':
    echo ini_get('max_execution_time');
    break;
case 'cpu':
    mark_status($k);
    set_time_limit(1);
    echo 'start';
    burn_cpu(3);
    echo 'survived';
    break;
case 'default':
    mark_status($k);
    echo 'start';
    burn_cpu(8);
    echo 'survived';
    break;
case 'sleep':
    set_time_limit(1);
    usleep(2000 * 1000);
    echo 'survived';
    break;
case 'restart':
    set_time_limit(1);
    burn_cpu(0.7);
    set_time_limit(1);
    burn_cpu(0.7);
    echo 'survived';
    break;
case 'zero':
    set_time_limit(0);
    burn_cpu(1.5);
    echo 'survived';
    break;
default:
    http_response_code(400);
    echo 'unknown case';
}
