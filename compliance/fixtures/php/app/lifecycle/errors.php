<?php
// Where error output goes under the pool's settings:
// php_admin_value[error_log] empty, log_errors on, php_admin_flag[display_errors] off.
// ini_get() returns "" or "0" for a boolean off, so display_* print on/off.
//   case=error_log        error_log() with message_type 0
//   case=error_log_sapi   error_log() with message_type 4
//   case=warning          an E_USER_WARNING, then "ok"
//   case=reporting_off    error_reporting(0), the same warning, then "ok"
//   case=display_ini      ini_get('display_errors')
//   case=display_set      ini_set('display_errors', '1') on an admin value
//   case=uncaught         an uncaught RuntimeException after "before"
//   case=handler          set_exception_handler() answers 500
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/lifecycle/errors');

switch ($q['case'] ?? '') {
case 'error_log':
    $r = error_log('ERRLOG-MARKER-0');
    echo 'ok:' . var_export($r, true);
    break;
case 'error_log_sapi':
    $r = error_log('ERRLOG-MARKER-4', 4);
    echo 'ok:' . var_export($r, true);
    break;
case 'warning':
    trigger_error('WARN-MARKER-5d1', E_USER_WARNING);
    echo 'ok';
    break;
case 'reporting_off':
    error_reporting(0);
    trigger_error('WARN-MARKER-5d1', E_USER_WARNING);
    echo 'ok';
    break;
case 'display_ini':
    echo ini_get('display_errors') ? 'on' : 'off';
    break;
case 'display_set':
    $r = ini_set('display_errors', '1');
    echo var_export($r, true) . ':' . (ini_get('display_errors') ? 'on' : 'off');
    break;
case 'uncaught':
    echo 'before';
    throw new RuntimeException('UNCAUGHT-MARKER-77');
case 'handler':
    set_exception_handler(function (Throwable $e) {
        http_response_code(500);
        echo 'handled:' . $e->getMessage();
    });
    echo 'before;';
    throw new RuntimeException('boom');
default:
    http_response_code(400);
    echo 'unknown case';
}
