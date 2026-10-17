<?php
// Diagnostics, uncaught throwables and their effect on the response.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/runtime/errors');
header('Content-Type: text/plain');

final class ThrowsOnDestruct
{
    public function __destruct()
    {
        throw new RuntimeException('from destructor');
    }
}

switch ($q['op'] ?? '') {
    case 'user_warning':
        echo 'before;';
        $r = trigger_error('fixture user warning', E_USER_WARNING);
        echo 'after:', var_export($r, true);
        break;
    case 'user_warning_last':
        @trigger_error('fixture user warning', E_USER_WARNING);
        $e = error_get_last();
        echo $e['type'], ':', $e['message'];
        break;
    case 'uncaught_error':
        echo 'before';
        no_such_function_in_fixture();
        echo 'after';
        break;
    case 'uncaught_exception':
        echo 'before';
        throw new LogicException('fixture uncaught');
    case 'user_error':
        echo 'before';
        trigger_error('fixture user error', E_USER_ERROR);
        echo 'after';
        break;
    case 'exception_handler':
        set_exception_handler(static function (Throwable $t): void {
            http_response_code(503);
            echo 'handled:', $t::class, ':', $t->getMessage();
        });
        throw new DomainException('to handler');
    case 'destructor_shutdown':
        // Destroyed during script termination: the manual says this is fatal.
        $GLOBALS['keep'] = new ThrowsOnDestruct();
        echo 'body';
        break;
    case 'json_throw':
        echo json_encode("\xB1\x31", JSON_THROW_ON_ERROR);
        break;
    case 'json_throw_caught':
        try {
            json_encode("\xB1\x31", JSON_THROW_ON_ERROR);
        } catch (JsonException $e) {
            echo $e::class, ':', $e->getCode(), ':', $e->getMessage();
        }
        break;
    default:
        http_response_code(400);
        echo 'unknown op';
}
