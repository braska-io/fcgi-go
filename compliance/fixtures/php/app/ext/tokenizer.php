<?php
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/ext/tokenizer');
header('Content-Type: text/plain');

switch ($q['op'] ?? '') {
    case 'phptoken':
        // php.net PhpToken::tokenize example #1.
        $tokens = PhpToken::tokenize('<?php echo; ?>');
        foreach ($tokens as $token) {
            echo "Line {$token->line}: {$token->getTokenName()} ('{$token->text}')", PHP_EOL;
        }
        break;
    default:
        http_response_code(400);
        echo 'unknown op';
}
