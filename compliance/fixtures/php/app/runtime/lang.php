<?php
// Language features that keep state inside one request: generators, fibers.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/runtime/lang');
header('Content-Type: text/plain');

switch ($q['op'] ?? '') {
    case 'generator':
        // php.net, language.generators.overview, example #1 (adapted to 1..3).
        $gen = (function () {
            for ($i = 1; $i <= 3; $i++) {
                yield $i;
            }
        })();
        foreach ($gen as $value) {
            echo "$value\n";
        }
        break;
    case 'fiber':
        // php.net, language.fibers, example #1.
        $fiber = new Fiber(function (): void {
            $value = Fiber::suspend('fiber');
            echo "Value used to resume fiber: ", $value, PHP_EOL;
        });
        $value = $fiber->start();
        echo "Value from fiber suspending: ", $value, PHP_EOL;
        $fiber->resume('test');
        break;
    default:
        http_response_code(400);
        echo 'unknown op';
}
