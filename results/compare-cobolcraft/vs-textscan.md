# gen-system against textscan

178 files compared; 0 rejected by textscan and left out.

| | textscan | gen-system | both | only textscan | only gen-system |
|---|---|---|---|---|---|
| CALL and PERFORM sites in the file | 3022 | 3054 | 3020 (99.9% of textscan) | 2 | 34 |
| sites from copybooks, by file and target | 0 | 1078 | 0 | 0 | 1078 |

Resolution on matched sites: call_outside_ok 83, call_to_program_ok 2514, perform_ok 423.

Left out of gen-system: 407 FUNCTION; 0 edges matched no CALL, PERFORM, FUNCTION or COPY on their line.

## Only textscan (2)

- `src/parsers/recipe/shaped.cob:155` PERFORM PATTERN-SLOT-OPTIONS: `PERFORM PATTERN-SLOT-OPTIONS(MULTI-CHOICE-SLOT-INDEX) TIMES`
- `src/parsers/recipe/shaped.cob:217` PERFORM TAGS-REGISTRY-TAG-LENGTH: `PERFORM TAGS-REGISTRY-TAG-LENGTH(TAGS-ITEM-REGISTRY-INDEX, TAG-INDEX) TIMES`

## Only gen-system (34)

- `codegen/common/templates.cob:139` CALL <DYNAMIC>: `CALL LK-TPL-REPLACE USING LK-TPL-VAR-NAME(VAR-INDEX) LK-OUT-BUFFER LK-OUT-LENGTH`
- `src/blocks/crafting_table.cob:49` CALL <DYNAMIC>: `CALL WINDOW-SYNC-PTR USING LK-PLAYER`
- `src/blocks/generic.cob:76` CALL <DYNAMIC>: `CALL LOOT-PTR USING LK-POSITION BLOCK-STATE SURVIVES-EXPLOSION`
- `src/commands.cob:132` CALL <DYNAMIC>: `CALL PTR USING LK-CLIENT-ID PARTS PRINT-USAGE`
- `src/entities/item.cob:262` CALL <DYNAMIC>: `CALL REPLACEABLE-PTR USING BLOCK-POSITION REPLACEABLE`
- `src/inventory/inventory.cob:19` CALL <DYNAMIC>: `CALL SYNC-PTR USING LK-PLAYER`
- `src/items/bed.cob:95` CALL <DYNAMIC>: `CALL CB-PTR-REPLACEABLE USING BLOCK-ID CHECK-RESULT`
- `src/items/common.cob:25` CALL <DYNAMIC>: `CALL CB-PTR-REPLACEABLE USING BLOCK-STATE CHECK-RESULT`
- `src/items/common.cob:40` CALL <DYNAMIC>: `CALL CB-PTR-REPLACEABLE USING BLOCK-STATE CHECK-RESULT`
- `src/items/door.cob:86` CALL <DYNAMIC>: `CALL CB-PTR-REPLACEABLE USING BLOCK-STATE CHECK-RESULT`
- `src/items/slab.cob:88` CALL <DYNAMIC>: `CALL CB-PTR-REPLACEABLE USING BLOCK-STATE CHECK-RESULT`
- `src/items/slab.cob:117` CALL <DYNAMIC>: `CALL CB-PTR-REPLACEABLE USING BLOCK-STATE CHECK-RESULT`
- `src/items/torch.cob:61` CALL <DYNAMIC>: `CALL FACE-CALLBACK USING BLOCK-STATE "up" BLOCK-FACE`
- `src/items/torch.cob:87` CALL <DYNAMIC>: `CALL FACE-CALLBACK USING BLOCK-STATE FACING BLOCK-FACE`
- `src/packets.cob:253` CALL <DYNAMIC>: `CALL HANDLER-PTR USING LK-CLIENT LK-PAYLOAD LK-OFFSET`
- `src/packets/clientbound/play/block-entity-data.cob:41` CALL <DYNAMIC>: `CALL SERIALIZE-PTR USING LK-BLOCK-ENTITY NBTENC PAYLOAD`
- `src/packets/clientbound/play/chunkdata.cob:123` CALL <DYNAMIC>: `CALL SERIALIZE-PTR USING LK-BLOCK-ENTITY(BLOCK-INDEX) NBTENC PAYLOAD`
- `src/packets/serverbound/play/container-click.cob:56` CALL <DYNAMIC>: `CALL SYNC-PTR USING PLAYER-ID`
- `src/packets/serverbound/play/container-click.cob:83` CALL <DYNAMIC>: `CALL DROP-PTR USING PLAYER-ID SLOT BUTTON SYNC-REQUIRED`
- `src/packets/serverbound/play/container-click.cob:102` CALL <DYNAMIC>: `CALL SET-SLOT-PTR USING PLAYER-ID SLOT-NUMBER CLIENT-SLOT SLOT-SYNC-REQUIRED`
- `src/packets/serverbound/play/container-click.cob:112` CALL <DYNAMIC>: `CALL SYNC-PTR USING PLAYER-ID`
- `src/packets/serverbound/play/container-close.cob:28` CALL <DYNAMIC>: `CALL CLOSE-PTR USING PLAYER-ID`
- `src/packets/serverbound/play/pick-block.cob:44` CALL <DYNAMIC>: `CALL BLOCK-ITEM-CALLBACK USING BLOCK-STATE IDENTIFIER`
- `src/packets/serverbound/play/player-action.cob:119` CALL <DYNAMIC>: `CALL CALLBACK-PTR USING PLAYER-ID LOCATION BLOCK-FACE`
- `src/packets/serverbound/play/use-item-on.cob:76` CALL <DYNAMIC>: `CALL CALLBACK-PTR-ITEM USING PLAYER-ID SLOT-INDEX ITEM-IDENTIFIER LOCATION FACE-ENUM CURSOR-POS`
- ... 9 more in the JSON report

## From copybooks, only gen-system (1078)

- `src/packets/clientbound/configuration/feature-flags.cob` PACKETS-GETID
- `src/packets/clientbound/configuration/finish-configuration.cob` PACKETS-GETID
- `src/packets/clientbound/configuration/known-packs.cob` PACKETS-GETID
- `src/packets/clientbound/configuration/registry.cob` PACKETS-GETID
- `src/packets/clientbound/configuration/update-tags.cob` PACKETS-GETID
- `src/packets/clientbound/disconnect.cob` PACKETS-GETID
- `src/packets/clientbound/disconnect.cob` PACKETS-GETID
- `src/packets/clientbound/disconnect.cob` PACKETS-GETID
- `src/packets/clientbound/login/login-success.cob` PACKETS-GETID
- `src/packets/clientbound/play/ack-block-change.cob` PACKETS-GETID
- `src/packets/clientbound/play/add-player.cob` PACKETS-GETID
- `src/packets/clientbound/play/block-destruction.cob` PACKETS-GETID
- `src/packets/clientbound/play/block-entity-data.cob` PACKETS-GETID
- `src/packets/clientbound/play/block-update.cob` PACKETS-GETID
- `src/packets/clientbound/play/bundle-delimiter.cob` PACKETS-GETID
- `src/packets/clientbound/play/chunkdata.cob` PACKETS-GETID
- `src/packets/clientbound/play/commands.cob` PACKETS-GETID
- `src/packets/clientbound/play/debug-sample.cob` PACKETS-GETID
- `src/packets/clientbound/play/entity-animation.cob` PACKETS-GETID
- `src/packets/clientbound/play/entity-event.cob` PACKETS-GETID
- `src/packets/clientbound/play/entity-position-sync.cob` PACKETS-GETID
- `src/packets/clientbound/play/entity-sound.cob` PACKETS-GETID
- `src/packets/clientbound/play/game-event.cob` PACKETS-GETID
- `src/packets/clientbound/play/keepalive.cob` PACKETS-GETID
- `src/packets/clientbound/play/login-play.cob` PACKETS-GETID
- ... 1053 more in the JSON report
