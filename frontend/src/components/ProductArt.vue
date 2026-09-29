<script setup lang="ts">
defineProps<{ type: string; large?: boolean }>();
</script>
<template>
  <div class="product-art" :class="[type, { large }]" aria-hidden="true">
    <svg viewBox="0 0 400 320" fill="none">
      <ellipse cx="200" cy="265" rx="110" ry="14" fill="#253d3514" />
      <g
        v-if="type === 'keyboard'"
        transform="translate(40 90) rotate(-9 160 80)"
      >
        <path
          d="M5 5h305q15 0 15 15v122q0 15-15 15H5q-15 0-15-15V20Q-10 5 5 5"
          fill="#b1b5ad"
        />
        <rect x="-10" y="-4" width="335" height="151" rx="17" fill="#d5d8cc" />
        <rect x="0" y="7" width="314" height="122" rx="10" fill="#a6b5a5" />
        <g v-for="row in 4" :key="row">
          <rect
            v-for="col in 11"
            :key="col"
            :x="6 + (col - 1) * 27.5"
            :y="12 + (row - 1) * 24.5"
            width="23"
            height="20"
            rx="4"
            :fill="
              col === 11 || (row === 1 && col === 1)
                ? '#e5a778'
                : col > 8
                  ? '#b2c1aa'
                  : '#f5f2e7'
            "
          />
          <path
            v-for="col in 10"
            :key="'s' + col"
            :d="`M${14 + (col - 1) * 27.5} ${20 + (row - 1) * 24.5}h5`"
            stroke="#9fa99c"
            stroke-width="1.4"
          />
        </g>
        <rect x="6" y="111" width="35" height="19" rx="4" fill="#f5f2e7" />
        <rect x="47" y="111" width="35" height="19" rx="4" fill="#f5f2e7" />
        <rect x="88" y="111" width="133" height="19" rx="4" fill="#f5f2e7" />
        <rect x="227" y="111" width="79" height="19" rx="4" fill="#b2c1aa" />
      </g>
      <g v-else-if="type === 'headphones'" transform="rotate(-13 200 160)">
        <path
          d="M105 184v-48a95 95 0 0 1 190 0v48"
          stroke="#95a89a"
          stroke-width="27"
        />
        <path
          d="M111 144v-8a89 89 0 0 1 178 0v8"
          stroke="#d1dccc"
          stroke-width="16"
        />
        <rect x="80" y="154" width="62" height="111" rx="30" fill="#d9e1d5" />
        <rect x="88" y="164" width="41" height="91" rx="20" fill="#a8b9a6" />
        <rect x="258" y="154" width="62" height="111" rx="30" fill="#d9e1d5" />
        <rect x="271" y="164" width="41" height="91" rx="20" fill="#a8b9a6" />
        <path
          d="M100 173v36 M282 173v36"
          stroke="#edf0e8"
          stroke-width="4"
          stroke-linecap="round"
        />
      </g>
      <g v-else-if="type === 'lamp'">
        <ellipse cx="202" cy="258" rx="71" ry="12" fill="#b4b9a3" />
        <path d="M146 250c2-25 111-25 113 0v7H146Z" fill="#d5d8bc" />
        <rect x="195" y="133" width="14" height="117" rx="7" fill="#b6bf9f" />
        <path
          d="M99 158c0-68 45-112 103-112s103 44 103 112H99Z"
          fill="#cdd5b9"
        />
        <ellipse cx="202" cy="158" rx="103" ry="15" fill="#f6ebc5" />
        <ellipse cx="202" cy="160" rx="48" ry="6" fill="#fffae0" />
        <path
          d="M157 75c-23 14-35 36-37 55"
          stroke="#e6ead6"
          stroke-width="10"
          stroke-linecap="round"
        />
      </g>
      <g v-else-if="type === 'speaker'" transform="rotate(-8 200 170)">
        <path
          d="M268 99c57-19 67 58 23 73"
          stroke="#b79e82"
          stroke-width="10"
        />
        <rect x="95" y="90" width="210" height="168" rx="53" fill="#b7c4b1" />
        <rect x="105" y="106" width="190" height="136" rx="43" fill="#c3ceba" />
        <g v-for="row in 13" :key="row">
          <circle
            v-for="col in 19"
            :key="col"
            :cx="119 + col * 8"
            :cy="115 + row * 8"
            r="1.4"
            fill="#869a7d"
          />
        </g>
        <rect x="174" y="79" width="50" height="13" rx="6" fill="#899d80" />
        <text
          x="200"
          y="181"
          text-anchor="middle"
          fill="#405d3f"
          font-size="12"
          font-weight="700"
          letter-spacing="3"
        >
          WAVE
        </text>
      </g>
      <g v-else-if="type === 'bag'">
        <path d="M173 74V59q28-24 54 0v15" stroke="#696e4f" stroke-width="12" />
        <path
          d="M111 154q-38 48-20 101 M287 154q38 48 20 101"
          stroke="#858569"
          stroke-width="17"
        />
        <rect x="112" y="66" width="179" height="204" rx="39" fill="#a7ac89" />
        <path d="M128 136V98q0-22 28-22h91q28 0 28 22v38" fill="#b5b99a" />
        <rect x="130" y="160" width="144" height="80" rx="17" fill="#969f79" />
        <path d="M145 177h116" stroke="#626d50" stroke-width="3" />
        <rect x="184" y="108" width="37" height="19" rx="3" fill="#ded5ba" />
        <path
          d="M256 178v14"
          stroke="#d5caac"
          stroke-width="5"
          stroke-linecap="round"
        />
      </g>
      <g v-else-if="type === 'bottle'" transform="rotate(13 200 160)">
        <rect x="158" y="41" width="85" height="36" rx="12" fill="#8a9c79" />
        <rect x="151" y="69" width="99" height="195" rx="29" fill="#c4cdb4" />
        <path
          d="M164 87v139q0 17 9 20"
          stroke="#e0e6d4"
          stroke-width="9"
          stroke-linecap="round"
        />
        <path d="M152 92h97" stroke="#a2b090" stroke-width="3" />
        <text
          x="202"
          y="182"
          text-anchor="middle"
          font-size="17"
          fill="#657458"
          letter-spacing="4"
        >
          SIP
        </text>
      </g>
      <g v-else-if="type === 'watch'" transform="rotate(24 200 165)">
        <rect x="161" y="15" width="77" height="290" rx="20" fill="#bfa58b" />
        <path
          d="M173 20v280 M226 20v280"
          stroke="#ddcbb7"
          stroke-width="2"
          stroke-dasharray="4 4"
        />
        <rect x="132" y="94" width="139" height="148" rx="43" fill="#b2b5a7" />
        <rect x="141" y="103" width="121" height="129" rx="35" fill="#f1f0df" />
        <rect x="271" y="143" width="10" height="22" rx="4" fill="#9d9e8e" />
        <circle cx="201" cy="168" r="45" stroke="#d6d9c4" stroke-width="1" />
        <path
          d="M201 134v35l26 12"
          stroke="#67775c"
          stroke-width="5"
          stroke-linecap="round"
        />
        <circle cx="201" cy="168" r="5" fill="#c28a5f" />
      </g>
      <g v-else-if="type === 'camera'" transform="rotate(-9 200 170)">
        <path d="M143 101 156 74h72l13 27" fill="#979f90" />
        <rect x="75" y="103" width="251" height="143" rx="24" fill="#969f93" />
        <rect x="75" y="126" width="251" height="105" rx="15" fill="#c1c9b8" />
        <rect x="266" y="91" width="33" height="16" rx="6" fill="#b18f70" />
        <rect x="97" y="141" width="31" height="14" rx="3" fill="#455b50" />
        <circle cx="212" cy="174" r="65" fill="#d9ddd0" />
        <circle cx="212" cy="174" r="54" fill="#718579" />
        <circle cx="212" cy="174" r="40" fill="#2b4941" />
        <circle cx="212" cy="174" r="29" fill="#17372f" />
        <circle cx="198" cy="160" r="12" fill="#53796d" />
        <circle cx="299" cy="123" r="4" fill="#de9764" />
      </g>
      <g v-else transform="rotate(-16 200 160)">
        <path
          d="M126 176c0-154 148-154 148 0v32c0 94-148 94-148 0v-32"
          fill="#c7ceba"
        />
        <path d="M200 71v105" stroke="#a0ac92" stroke-width="2" />
        <rect x="192" y="105" width="16" height="36" rx="8" fill="#879b78" />
        <path
          d="M142 165c0-42 7-61 18-74"
          stroke="#e5e8d9"
          stroke-width="8"
          stroke-linecap="round"
        />
        <path d="M127 180h146" stroke="#bbc4ac" stroke-width="2" />
      </g>
    </svg>
  </div>
</template>
