<?php
// register_shutdown_function() ordering and the paths that reach it.
//   case=order          a, b registered; body "Mab"
//   case=exit_in_first  a calls exit; body "Ma"
//   case=nested         a registers c; body "Mabc"
//   case=after_exit     exit('X') with a shutdown function echoing "S"
//   case=with_destructor  the exit() manual example: shutdown, then destructor
//   case=fatal&k=K      call to an undefined function; the shutdown function
//                       echoes SHUTDOWN-ECHO and marks status and last error
//   case=exception&k=K  uncaught RuntimeException; same shutdown function
require __DIR__ . '/../../_fixture.php';
require __DIR__ . '/../pdo/_db.php';
$q = fixture('app/lifecycle/shutdown');
$k = $q['k'] ?? '';

class Foo
{
    public function __destruct()
    {
        echo 'Destruct: ' . __METHOD__ . '()' . PHP_EOL;
    }
}

function shutdown()
{
    echo 'Shutdown: ' . __FUNCTION__ . '()' . PHP_EOL;
}

function observe_shutdown(string $k): void
{
    register_shutdown_function(function () use ($k) {
        echo 'SHUTDOWN-ECHO';
        $last = error_get_last();
        mark($k, 'shutdown status=' . connection_status() . ' last=' . ($last['type'] ?? 'none'));
    });
}

switch ($q['case'] ?? '') {
case 'order':
    register_shutdown_function(function () { echo 'a'; });
    register_shutdown_function(function () { echo 'b'; });
    echo 'M';
    break;
case 'exit_in_first':
    register_shutdown_function(function () { echo 'a'; exit; });
    register_shutdown_function(function () { echo 'b'; });
    echo 'M';
    break;
case 'nested':
    register_shutdown_function(function () {
        echo 'a';
        register_shutdown_function(function () { echo 'c'; });
    });
    register_shutdown_function(function () { echo 'b'; });
    echo 'M';
    break;
case 'after_exit':
    register_shutdown_function(function () { echo 'S'; });
    exit('X');
case 'with_destructor':
    $foo = new Foo();
    register_shutdown_function('shutdown');
    exit();
case 'fatal':
    observe_shutdown($k);
    echo 'before';
    no_such_function_for_the_suite();
    echo 'after';
    break;
case 'exception':
    observe_shutdown($k);
    echo 'before';
    throw new RuntimeException('shutdown fixture');
default:
    http_response_code(400);
    echo 'unknown case';
}
