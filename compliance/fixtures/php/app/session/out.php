<?php
// echo, print and output buffering.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/session/out');

function apples_to_oranges(string $buffer): string
{
    return str_replace('apples', 'oranges', $buffer);
}

switch ($q['case'] ?? 'echo') {
    case 'echo':
        echo 'a';
        print 'b';
        echo 'c', 'd';
        printf('%d', 1);
        break;
    case 'empty':
        break;
    case 'ob_nest':
        ob_start();
        echo 'outer-';
        ob_start();
        echo 'inner';
        $inner = ob_get_clean();
        echo strtoupper($inner);
        ob_end_flush();
        break;
    case 'ob_callback':
        ob_start('apples_to_oranges');
        echo "It's like comparing apples to oranges.";
        ob_end_flush();
        break;
    case 'ob_callback_at_end':
        // No ob_end_flush(): the buffer is flushed through the callback at script end.
        ob_start('apples_to_oranges');
        echo 'apples';
        break;
    case 'ob_get_clean':
        ob_start();
        echo 'Hello World';
        $out = ob_get_clean();
        $out = strtolower($out);
        var_dump($out);
        break;
    case 'ob_end_clean':
        echo 'keep';
        ob_start();
        echo 'drop';
        ob_end_clean();
        echo '!';
        break;
    case 'ob_clean_all':
        // Discards the ini-level buffer too: output before this point never leaves.
        echo 'drop';
        while (ob_get_level() > 0) {
            ob_end_clean();
        }
        echo 'kept';
        break;
    case 'ob_chunk':
        // chunk_size 1 flushes after every output operation.
        ob_start(fn ($b) => '[' . $b . ']', 1);
        echo 'x';
        echo 'y';
        ob_end_flush();
        break;
}
